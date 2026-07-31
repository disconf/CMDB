import { createRouter,createWebHistory,type RouteRecordRaw } from 'vue-router'
import type { Pinia } from 'pinia'
import { useAuthStore } from '@/stores/useAuthStore'

const RouteCanvas={template:'<span class="route-canvas" aria-hidden="true"></span>'}
const routes:RouteRecordRaw[]=[
  {path:'/login',name:'login',component:RouteCanvas,meta:{public:true}},
  {path:'/',name:'dashboard',component:RouteCanvas,meta:{permission:'dashboard:view'}},
  {path:'/cmdb',name:'cmdb',component:RouteCanvas,meta:{permission:'cmdb:view'}},{path:'/discovery',name:'discovery',component:RouteCanvas,meta:{permission:'discovery:view'}},{path:'/topology',name:'topology',component:RouteCanvas,meta:{permission:'topology:view'}},{path:'/monitor',name:'monitor',component:RouteCanvas,meta:{permission:'monitor:view'}},{path:'/jobs',name:'jobs',component:RouteCanvas,meta:{permission:'job:view'}},{path:'/tickets',name:'tickets',component:RouteCanvas,meta:{permission:'ticket:view'}},{path:'/releases',name:'releases',component:RouteCanvas,meta:{permission:'release:view'}},{path:'/toolbox',name:'toolbox',component:RouteCanvas,meta:{permission:'toolbox:use'}},{path:'/aiops',name:'aiops',component:RouteCanvas,meta:{permission:'aiops:view'}},{path:'/system',name:'system',component:RouteCanvas,meta:{permission:'system:manage'}},
  {path:'/:pathMatch(.*)*',redirect:'/'}]
export function createAppRouter(pinia:Pinia){ const router=createRouter({history:createWebHistory(),routes}); router.beforeEach(async to=>{const auth=useAuthStore(pinia);if(!auth.isReady)await auth.initialize();if(to.meta.public){return auth.isAuthenticated?{name:'dashboard'}:true}if(!auth.isAuthenticated)return{name:'login',query:{redirect:to.fullPath}};const permission=to.meta.permission as string|undefined;if(permission&&!auth.user?.permissions.includes(permission))return{name:'dashboard'};return true});return router }
