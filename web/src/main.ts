import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { message } from 'antdv-next'
import App from './App.vue'
import router from './router'
import i18n from './locales'
import './assets/styles/main.css'

const app = createApp(App)

// 渲染期异常默认只进控制台，用户面对的是无声白屏；这里给一次可见提示并限流，避免循环刷屏
let lastErrorReportAt = 0
app.config.errorHandler = (err, _instance, info) => {
  console.error('[yigate] 组件错误', info, err)
  const now = Date.now()
  if (now - lastErrorReportAt < 3000) return
  lastErrorReportAt = now
  message.error(i18n.global.t('feedback.renderError'))
}

app.use(createPinia())
app.use(i18n)
app.use(router)

app.mount('#app')
