package com.bingebandit.tv

import java.io.File
import java.io.RandomAccessFile
import java.nio.ByteBuffer
import java.nio.ByteOrder

/**
 * Reads the server addresses a BingeBandit server writes into the APK it hands
 * out at /tv (server/internal/api/tvapp.go). They sit in the APK Signing Block
 * under their own ID, which the signature doesn't cover, so the app finds out
 * which server it came from and can skip asking for an address.
 */
object ApkStamp {
  private const val STAMP_ID = 0x76734242
  private const val MAGIC = "APK Sig Block 42"

  /** The stamp's JSON, or null for an APK from GitHub or Google Play. */
  fun read(apk: File): String? = try {
    RandomAccessFile(apk, "r").use { f -> find(f) }
  } catch (e: Exception) {
    null
  }

  private fun find(f: RandomAccessFile): String? {
    val len = f.length()
    // The end of central directory record: 22 bytes plus a comment of up to 64 KB.
    val tailLen = minOf(len, 22L + 0xffff).toInt()
    val tail = ByteArray(tailLen)
    f.seek(len - tailLen)
    f.readFully(tail)
    val t = ByteBuffer.wrap(tail).order(ByteOrder.LITTLE_ENDIAN)
    var eocd = -1
    for (i in tailLen - 22 downTo 0) {
      if (t.getInt(i) == 0x06054b50 && (t.getShort(i + 20).toInt() and 0xffff) + i + 22 == tailLen) {
        eocd = i
        break
      }
    }
    if (eocd < 0) return null
    val cd = t.getInt(eocd + 16).toLong() and 0xffffffffL
    if (cd < 32) return null

    val footer = ByteArray(24)
    f.seek(cd - 24)
    f.readFully(footer)
    if (String(footer, 8, 16, Charsets.US_ASCII) != MAGIC) return null
    val size = ByteBuffer.wrap(footer).order(ByteOrder.LITTLE_ENDIAN).getLong(0)
    if (size < 24 || size > cd - 8 || size > 16L * 1024 * 1024) return null
    val block = ByteArray((size - 24).toInt())
    f.seek(cd - size)
    f.readFully(block)

    val b = ByteBuffer.wrap(block).order(ByteOrder.LITTLE_ENDIAN)
    while (b.remaining() >= 12) {
      val n = b.getLong()
      if (n < 4 || n > b.remaining()) return null
      val id = b.getInt()
      if (id == STAMP_ID) {
        val value = ByteArray((n - 4).toInt())
        b.get(value)
        return String(value, Charsets.UTF_8)
      }
      b.position(b.position() + (n - 4).toInt())
    }
    return null
  }
}
