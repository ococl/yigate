import { createRouter, createWebHistory } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'
import { AUTH_EXPIRED_EVENT, getToken } from '@/api/request'

const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    component: () => import('@/layouts/BlankLayout.vue'),
    children: [
      {
        path: '',
        name: 'login',
        component: () => import('@/views/login/LoginView.vue'),
      },
    ],
  },
  {
    path: '/',
    component: () => import('@/layouts/DefaultLayout.vue'),
    children: [
      {
        path: '',
        redirect: '/dashboard',
      },
      {
        path: 'dashboard',
        name: 'dashboard',
        component: () => import('@/views/dashboard/DashboardView.vue'),
      },
      {
        path: 'upstreams',
        name: 'upstreams',
        component: () => import('@/views/upstreams/UpstreamsView.vue'),
      },
      {
        path: 'rules',
        name: 'rules',
        component: () => import('@/views/rules/RulesView.vue'),
      },
      {
        path: 'geosite',
        name: 'geosite',
        component: () => import('@/views/geosite/GeoSiteView.vue'),
      },
      {
        path: 'users',
        name: 'users',
        component: () => import('@/views/users/UsersView.vue'),
      },
      {
        path: 'traffic',
        name: 'traffic',
        component: () => import('@/views/traffic/TrafficView.vue'),
      },
      {
        path: 'settings',
        name: 'settings',
        component: () => import('@/views/settings/SettingsView.vue'),
      },
      {
        path: 'pprof',
        name: 'pprof',
        component: () => import('@/views/pprof/PprofView.vue'),
      },
      {
        path: 'docs/auth',
        name: 'docs-auth',
        component: () => import('@/views/auth-doc/AuthDocView.vue'),
      },
      {
        path: 'docs/api',
        name: 'docs-api',
        component: () => import('@/views/api-doc/ApiDocView.vue'),
      },
      {
        path: 'about',
        name: 'about',
        component: () => import('@/views/about/AboutView.vue'),
      },
      {
        // 未匹配的前端路由落到 404 页面，避免空白工作区。
        path: ':pathMatch(.*)*',
        name: 'not-found',
        component: () => import('@/views/not-found/NotFoundView.vue'),
      },
    ],
  },
]

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes,
})

// 发布新版本后旧页面可能引用已删除的 chunk；自动刷新一次获取最新入口，
// 并用时间戳避免服务异常时陷入刷新循环。
router.onError((error, to) => {
  const text = String((error as Error)?.message || error)
  if (!/Failed to fetch dynamically imported module|Importing a module script failed|Loading chunk/i.test(text)) {
    return
  }
  const key = 'yigate_chunk_reload_at'
  const last = Number(sessionStorage.getItem(key) || 0)
  if (Date.now() - last < 10000) return
  sessionStorage.setItem(key, String(Date.now()))
  window.location.assign(to?.fullPath || window.location.href)
})

// 路由守卫
router.beforeEach((to) => {
  const token = getToken()
  if (to.name === 'login') {
    if (token) {
      return '/dashboard'
    }
    return true
  }
  if (!token) {
    return '/login'
  }
  return true
})

// API 层收到 401 后清除凭据并广播事件；这里完成 SPA 内跳转，
// 避免拦截器中硬刷新（window.location.href）造成状态丢失与无限循环。
if (typeof window !== 'undefined') {
  window.addEventListener(AUTH_EXPIRED_EVENT, () => {
    if (router.currentRoute.value.name !== 'login') {
      void router.replace('/login')
    }
  })
}

export default router
