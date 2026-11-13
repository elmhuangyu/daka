declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent<object, object, unknown>
  export default component
}

declare module 'virtual:emoji-zh-en-data' {
  /** URL of the Chinese emoji data that also carries English search terms. */
  const url: string
  export default url
}
