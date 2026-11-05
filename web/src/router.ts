import { createRouter, type RouterHistory } from 'vue-router'
import DownloadsPage from './pages/DownloadsPage.vue'
import LibraryPage from './pages/LibraryPage.vue'
import SearchPage from './pages/SearchPage.vue'
import StatusPage from './pages/StatusPage.vue'

// shelf answers every path outside /api with index.html, so the router owns
// unknown paths too.
export function createAppRouter(history: RouterHistory) {
  return createRouter({
    history,
    routes: [
      { path: '/', redirect: '/library' },
      { path: '/search', name: 'search', component: SearchPage },
      { path: '/downloads', name: 'downloads', component: DownloadsPage },
      { path: '/downloads/:id(\\d+)', name: 'download', component: DownloadsPage },
      { path: '/library', name: 'library', component: LibraryPage },
      { path: '/library/:id(\\d+)', name: 'album', component: LibraryPage },
      { path: '/status', name: 'status', component: StatusPage },
      { path: '/:pathMatch(.*)*', redirect: '/library' },
    ],
  })
}
