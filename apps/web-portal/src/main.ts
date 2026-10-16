import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { createAppRouter } from './router'
import './styles/main.css'
import './styles/auth.css'
import './styles/cmdb.css'
import './styles/cmdb-write.css'
import './styles/cmdb-models.css'
import './styles/cmdb-agent.css'
import './styles/cmdb-monitoring.css'
import './styles/discovery.css'
import './styles/topology-center.css'
import './styles/monitor.css'
import './styles/monitor-coverage.css'
import './styles/jobs.css'
import './styles/jobs-control.css'
import './styles/tickets.css'
import './styles/releases.css'
import './styles/toolbox.css'
import './styles/aiops.css'
import './styles/system.css'
import { clearAuthFragment } from '@/auth/keycloak'

const pinia=createPinia()
const router=createAppRouter(pinia)
const app=createApp(App).use(pinia).use(router)
void router.isReady().finally(()=>{app.mount('#app');clearAuthFragment()})
