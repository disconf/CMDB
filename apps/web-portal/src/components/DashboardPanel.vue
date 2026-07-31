<script setup lang="ts">
defineProps<{ title: string; action?: string }>()
defineEmits<{ action: [] }>()
</script>
<template><section class="dashboard-panel"><header><h2>{{ title }}</h2><button v-if="action" type="button" @click="$emit('action')">{{ action }} <span>›</span></button></header><div class="panel-body"><slot /></div></section></template>
