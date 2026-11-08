import { createRouter, createWebHistory, type RouterHistory } from 'vue-router'

export function makeRouter(history: RouterHistory = createWebHistory()) {
  return createRouter({
    history,
    routes: [
      { path: '/', name: 'today', component: () => import('./TodayView.vue') },
      { path: '/:pathMatch(.*)*', redirect: '/' },
    ],
  })
}
