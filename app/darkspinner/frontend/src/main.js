import { createApp } from 'vue'
import App from './App.vue'
import './style.css'
import { translate } from './localization'

const app = createApp(App)
app.config.globalProperties.$t = translate
app.mount('#app')
