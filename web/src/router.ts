import { createRouter, type RouterHistory } from 'vue-router'
import HomePage from './pages/HomePage.vue'

// shelf answers every path outside /api with index.html, so the router owns
// unknown paths too.
export function createAppRouter(history: RouterHistory) {
  return createRouter({
    history,
    routes: [{ path: '/:pathMatch(.*)*', name: 'home', component: HomePage }],
  })
}
