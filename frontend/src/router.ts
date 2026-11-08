import { createRouter, createWebHistory, type RouterHistory } from 'vue-router'
import { useAuthStore } from './stores/auth'

export function makeRouter(history: RouterHistory = createWebHistory()) {
  const router = createRouter({
    history,
    routes: [
      { path: '/login', name: 'login', component: () => import('./views/LoginView.vue'), meta: { public: true } },
      { path: '/', name: 'today', component: () => import('./TodayView.vue') },
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
