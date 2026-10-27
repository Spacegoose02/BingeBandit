package store

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

// shipped holds the SHA-256 of every migration a release has run. A server
// that already ran one never runs it again, so editing it splits installs in
// two: v0.22.0 moved migration 43's users_email index into 44, and every
// server upgrading from v0.21.0 failed at start with "index users_email
// already exists". Add new migrations at the end and append their hash here;
// never change an old one.
var shipped = []string{
	"3730155e9ffa09e631f6803f856d2630392d3df349adbf9250b8129158900b0c",
	"f28db75abcbfbd49e30df0d7beb8248f7b236ee6dd630c2d52dd40a1bb9f930f",
	"3f45b7758b55ef50467cb5601426bef8da10909c2dec942f9bc76dd6197e34d5",
	"3ab456fdc1ca2554c8f7a4ca38f73032db295e13ab86bbcc1c273fde46b58d1f",
	"679e492e165327ee1114470d0a82aa6818fdebc930fa2a1965fb62401d74d454",
	"d4816965b7b07ef302c37720db049158a0bfeaf7235e5e1f51a77fb2149c32ef",
	"7f77c7a076d69d1cbe15fc6f132dfe16e881f569b2e15de7ccf1fab9f741014e",
	"5b2b132e67c30a7ab2bd1c4106cdac23985f122baff6d8da502781e40c12fbdf",
	"62b803e10b83344fbb30962db496e92bf1b9cbecf3f6ba5f878ab868148b82cc",
	"d19a4886629fd0d1a566dea43bbac8f41b2d3bd92a9f34dd5af8b43cc1b423bc",
	"d6e3ce562d6cd8bd49fca9ad938484b6e4ea547aedfc5fe7dbb892e427857ccd",
	"eb47b9f44a6b7e07fe0bce64c365e0e594d0dd66bb2bf4f9593cbb5c13119d79",
	"f8fb96a83d636fecc0b9a0a453cf0178f186d2fadb37dfbdb1a85218152e4ff6",
	"e4718efab6b1d49960f0cd2daf2583258c1871b3aaeb0e1c9ae8b018537d8589",
	"edef6fb596ad12729ef7095eb0a5883555b083e6bd9855a6df78a16d650afb88",
	"f47334fdbf4910d59b3c8776272c66e41498ca4fd9897082529064bfd951dc71",
	"c2a74494bdaca8698fce4cd3af9e723765b4887dc3bde4bb7d709ff223f70219",
	"2d288359d9d89cad9720bf4ec4ed6ff44c4a8e7b96de76df1337e4eac588d4d3",
	"71bf1234171f1f840ff6c0cda10f2000222c3d66aecf66f189bf64dce4748c3b",
	"11c00dda8915aff91573d28e590dcdbd62dcc5d488cb8213c4d0659c7ae1de69",
	"48db53eeb4ca7645a3decca6afbf6495a18017b2fac99df8bb6bea7dcae3b7e2",
	"b2dc9d270ab6a19711c033f8055a269e8b674ab3a9703cb4795570a555f7765a",
	"67079bd0a9ba650573cff1609bf0cdc26a93ebfad55fc082b3e5a2f7b234616a",
	"1c44759c73f3cc0285834ee7d00f2248696c9e49d06037b72b2e5fc964c42e08",
	"4c8efad00af5df9ef9dad77b2c43c295b29fa2e9b4c68fe9a4a410cf365a844d",
	"c4bc9369df258da55740c209524d6302c24f8ccacfedb55687874907910f8ad3",
	"9719988aed8c357fea51e409b6558e5b1624cb64afaa59f3b7f8fa99e735e1b2",
	"efc5b84a481ba8d46329e8adae50dc90b80552f07dc303d877b7e61b0b7a94bc",
	"9d0b0a19ed83671b63d165e9523f5145b78aa9560c48249f01946ac2b6bb8a7e",
	"09634383220cb5a5e4732ec8370d0af29233f0300bee7d84fca3b144827b28cd",
	"70b45a61e8d8d6cbb7ff8fce0369896849dbeb0a95c396bc85db837cdffad13a",
	"de5324a24b9fa5ce5e973773896bebd0ed39e2bba6da1b1c1c6528896fab7953",
	"306782057a8319ca2a48f75bcca382fef1b1d2bae7abe8abcb8ca00a06a9134e",
	"68e55520d87fed6aad3d2de1efd76c2b0d2b33bb2829a583d49ccf367c2f0750",
	"d99911370d0b8a4fa98904369c515aa86202aa7fc54255f251f7695e4091f8fa",
	"ede608cb2ceeec631daa36650168bc90aea6853b335cd633f733386464bfde32",
	"1d77d7aea70008a15fad550de561c4e10bdd095468d2c21a80d7d24a4d41879c",
	"808a7b5d46727638604344149d3aa101fc96038ca8396091803af1d13540f416",
	"72a897cf5e5bd44c838ee517847a6d577e4e202591bede8f171f25f2837942e0",
	"4b5ba041bfed90788a0097ecb0e4f3816615cabdf08187b9e0dd2199f87d0d2c",
	"4efd7cb732ce3ccb604d30f796ae8aac15c1f037eda34c053cec65a812f5bf19",
	"ec66f8427ec7612831e0563ec8e608654fbda952bb0cb46dce612e0bc14acde0",
	"f7c76b9a7e856ae802a8e952b9b1491c9acc6de1adfd0cf0db86c4cfda3008e0",
	"25fd05469f325854663a4a7e0eea91704f52d89f628d78bc609a6e388b137659",
	"6133c331b54fefa7a4de50491a67a81d66af1119b3ee80708d1ae1d42f223c80",
	"c263b91b42c49a1667efd7b4ae61743e8f53af052301dcff39afb107fc8bcf9f",
}

func TestShippedMigrationsUnchanged(t *testing.T) {
	if len(migrations) < len(shipped) {
		t.Fatalf("%d migrations, but %d have shipped", len(migrations), len(shipped))
	}
	for i, want := range shipped {
		if got := fmt.Sprintf("%x", sha256.Sum256([]byte(migrations[i]))); got != want {
			t.Errorf("migration %d changed after it shipped; add a new migration instead", i+1)
		}
	}
}

// Every database a release can leave behind must upgrade to the latest.
func TestMigrateFromEveryVersion(t *testing.T) {
	for stop := 1; stop < len(migrations); stop++ {
		path := filepath.Join(t.TempDir(), "bb.db")
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < stop; i++ {
			if _, err := db.Exec(migrations[i]); err != nil {
				t.Fatalf("migration %d: %v", i+1, err)
			}
		}
		if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version=%d", stop)); err != nil {
			t.Fatal(err)
		}
		db.Close()
		s, err := Open(path)
		if err != nil {
			t.Fatalf("upgrade from %d: %v", stop, err)
		}
		s.Close()
	}
}
