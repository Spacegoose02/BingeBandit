// @ts-check
// Picking the server: type an address, or pick one found on the network.
// window.bbSetup comes from src/preload.js.

/** @type {{ probe(input: string): Promise<any>, scan(): Promise<any[]>, use(url: string): Promise<void>, updates: any, startup?: { get(): Promise<boolean | null>, set(on: boolean): Promise<boolean | null> } }} */
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

// Starting with Windows (only the installed or portable app can).
setup.startup?.get().then((on) => {
  if (on === null || on === undefined) return
  $('startup').checked = on
  $('startup-row').hidden = false
})
$('startup').addEventListener('change', async () => {
  const on = await setup.startup.set($('startup').checked)
  if (on !== null) $('startup').checked = on
})
scan()

// The app's version, and a new one when it's ready (src/updates.js).
$('version').textContent = `BingeBandit for Windows ${setup.updates.version}`
function showUpdate(u) {
  const ready = u?.status === 'ready'
  const available = u?.status === 'available'
  $('update').hidden = !ready && !available
  $('update-text').textContent = ready ? `Version ${u.version} is ready.` : available ? `Version ${u.version} is out.` : ''
  $('update-go').textContent = ready ? 'Restart now' : 'Download'
}
$('update-go').addEventListener('click', () => setup.updates.install())
setup.updates.state().then(showUpdate)
setup.updates.onChange(showUpdate)
