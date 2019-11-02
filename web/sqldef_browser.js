/* global WebAssembly, fetch, Go,_SQLDEF */
window.sqldef = async (dbType, desiredDDLs, currentDDLs) => {
  if (WebAssembly) {
    if (WebAssembly && !WebAssembly.instantiateStreaming) { // polyfill
      WebAssembly.instantiateStreaming = async (resp, importObject) => {
        const source = await (await resp).arrayBuffer()
        return WebAssembly.instantiate(source, importObject)
      }
    }
    const go = new Go()
    const result = await WebAssembly.instantiateStreaming(fetch('sqldef.wasm'), go.importObject)
    go.run(result.instance)
    return new Promise((resolve, reject) => {
      _SQLDEF(dbType, desiredDDLs, currentDDLs, (err, ret) => {
        if (err) {
          return reject(err)
        }
        resolve(ret)
      })
    })
  } else {
    throw new Error('WebAssembly is not supported in your browser')
  }
}
