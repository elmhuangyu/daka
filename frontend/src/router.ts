import { createRouter, createWebHistory, type RouterHistory } from 'vue-router'
import { useAuthStore } from './stores/auth'

declare module 'vue-router' {
  interface RouteMeta {
    /** Reachable without a session; rendered without the app layout. */
    public?: boolean
    title?: string
  }
}

// meta.title is not rendered anywhere (kept as route metadata only, so it is not translated).
export function makeRouter(history: RouterHistory = createWebHistory()) {
  const router = createRouter({
    history,
    routes: [
      { path: '/login', name: 'login', component: () => import('./views/LoginView.vue'), meta: { public: true } },
      { path: '/', name: 'today', component: () => import('./views/TodayView.vue'), meta: { title: 'Today' } },
      { path: '/calendar', name: 'calendar', component: () => import('./views/CalendarView.vue'), meta: { title: 'Calendar' } },
      {
        path: '/items', component: () => import('./views/ItemsView.vue'), meta: { title: 'Items' },
        children: [
          { path: '', name: 'items', component: () => import('./views/ItemsEmpty.vue'), meta: { title: 'Items' } },
          { path: ':id(\\d+)', name: 'item-detail', component: () => import('./views/ItemDetailView.vue'), meta: { title: 'Item' } },
        ],
      },
      { path: '/items/new', name: 'item-new', component: () => import('./views/ItemEditorView.vue'), meta: { title: 'New item' } },
      {
        path: '/items/:id(\\d+)/edit', name: 'item-edit',
        component: () => import('./views/ItemEditorView.vue'), meta: { title: 'Edit item' },
      },
      { path: '/settings', name: 'settings', component: () => import('./views/SettingsView.vue'), meta: { title: 'Settings' } },
      { path: '/:pathMatch(.*)*', redirect: '/' },
    ],
  })
  router.beforeEach(async (to) => {
    const auth = useAuthStore()
    await auth.init()
    const needsLogin = auth.loginRequired && !auth.authenticated
    if (needsLogin && !to.meta.public) return { name: 'login' }
    if (!needsLogin && to.name === 'login') return { name: 'today' }
  })
  return router
}
