// @ts-check
// Picking the server: type an address, or pick one found on the network.
// window.bbSetup comes from src/preload.js.

/** @type {{ probe(input: string): Promise<any>, scan(): Promise<any[]>, use(url: string): Promise<void> }} */
const setup = /** @type {any} */ (window).bbSetup
const $ = (id) => /** @type {any} */ (document.getElementById(id))

const params = new URLSearchParams(location.search)
if (params.get('error')) {
  $('error').textContent = params.get('error')
  $('error').hidden = false
}
$('address').value = params.get('server') || ''

$('form').addEventListener('submit', async (e) => {
  e.preventDefault()
  const input = $('address').value.trim()
  if (!input) return
  $('connect').disabled = true
  $('status').textContent = 'Connecting…'
  const s = await setup.probe(input)
  $('connect').disabled = false
  if (s) {
    $('status').textContent = `Found ${s.name}.`
    setup.use(s.url)
  } else {
    $('status').textContent = `No BingeBandit server answered at ${input}.`
  }
})

async function scan() {
  const list = $('found')
  list.innerHTML = ''
  $('rescan').disabled = true
  $('status').textContent = 'Looking for servers on your network…'
  const servers = await setup.scan()
  $('rescan').disabled = false
  $('status').textContent = servers.length ? '' : 'No servers found on this network. Type the address instead.'
  for (const s of servers) {
    const li = document.createElement('li')
    const b = document.createElement('button')
    b.type = 'button'
    const name = document.createElement('span')
    name.textContent = s.name
    const where = document.createElement('small')
    where.textContent = s.url + (s.version ? ` · ${s.version}` : '')
    b.append(name, where)
    b.addEventListener('click', () => setup.use(s.url))
    li.append(b)
    list.append(li)
  }
}

$('rescan').addEventListener('click', scan)
scan()
