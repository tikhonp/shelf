import { createPinia } from 'pinia'
import { createApp } from 'vue'
import { createWebHistory } from 'vue-router'
import App from './App.vue'
import { createAppRouter } from './router'
import './styles/fonts.css'
import './styles/tokens.css'
import './styles/parts.css'
import './styles/library.css'
import './styles/inbox.css'
import './styles/layout.css'

createApp(App).use(createPinia()).use(createAppRouter(createWebHistory())).mount('#app')
