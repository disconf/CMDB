<script setup lang="ts">
import { ref } from 'vue'
import { Gauge,ShieldCheck } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/useAuthStore'

const auth=useAuthStore(),error=ref(''),isLoading=ref(false)
async function submit(){isLoading.value=true;error.value='';try{await auth.signIn()}catch(reason){error.value=reason instanceof Error?reason.message:'登录失败';isLoading.value=false}}
</script>
<template><main class="login-page"><div class="login-atmosphere"><div class="orbit orbit-one"></div><div class="orbit orbit-two"></div><div class="login-emblem"><Gauge/><span>CMDB</span></div></div><section class="login-card"><header><div class="login-logo"><Gauge/></div><div><p>INTELLIGENT OPERATIONS</p><h1>智能运维中心</h1></div></header><div class="login-status"><ShieldCheck/><span>Keycloak 统一身份认证</span><i></i><small>OIDC + PKCE</small></div><form @submit.prevent="submit"><p v-if="error" class="login-error">{{error}}</p><button type="submit" :disabled="isLoading">{{isLoading?'正在跳转认证中心…':'登录运维平台'}}</button></form><footer><span>账号与权限由统一身份认证中心管理</span></footer></section></main></template>
