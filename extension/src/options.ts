const token = document.querySelector<HTMLInputElement>('#token')!
const port = document.querySelector<HTMLInputElement>('#port')!
const output = document.querySelector<HTMLElement>('[role=status]')!
const form = document.querySelector<HTMLFormElement>('form')!

void Promise.all([
  chrome.storage.local.get(['apiToken', 'port']),
  chrome.storage.managed.get(['apiToken', 'port']).catch((): Record<string, unknown> => ({})),
]).then(([local, managed]) => {
  token.value = typeof local.apiToken === 'string' ? local.apiToken : ''
  port.value = String(managed.port ?? local.port ?? 8765)
  if (typeof managed.apiToken === 'string') {
    for (const field of form.querySelectorAll('input, button')) (field as HTMLInputElement).disabled = true
    output.textContent = 'החיבור מנוהל באמצעות מדיניות Chrome.'
  }
}).catch(() => { output.textContent = 'לא ניתן לטעון את ההגדרות.' })

form.addEventListener('submit', async event => {
  event.preventDefault()
  try {
    await chrome.storage.local.set({ apiToken: token.value, port: port.valueAsNumber })
    output.textContent = 'ההגדרות נשמרו.'
  } catch {
    output.textContent = 'שמירת ההגדרות נכשלה.'
  }
})
