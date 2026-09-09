<script setup lang="ts">
import { ref } from 'vue'
import { Download, UploadCloud, X } from 'lucide-vue-next'
import { parseAssetCsv } from '@/features/cmdb/assetForm'
import { fetchModelTemplate } from '@/api/cmdb'
import { useAuthStore } from '@/stores/useAuthStore'
import type { AssetInput, CiModel, ImportResult } from '@/features/cmdb/types'
const props = defineProps<{ result?: ImportResult; models?: CiModel[] }>()
const emit = defineEmits<{ close: []; import: [rows: AssetInput[], upsert: boolean] }>()
const auth = useAuthStore()
const upsert = ref(true)
const text = ref('id,name,type,status,ip,environment,projectGroup,owner,location,tags,bmc_ip,rack_no,u_position,serial\np1-srv-101,p1-web-101,physical-server,online,10.99.1.101,生产,核心系统组,张三,上海一号机房 / A03-12,测试|Linux,10.99.1.9,A03,12U,SN123456')
const templateCode = ref(props.models?.[0]?.code ?? 'physical-server')
const loadingTemplate = ref(false)
async function loadTemplate() {
  if (!templateCode.value) return
  loadingTemplate.value = true
  try { const header = await fetchModelTemplate(auth.token, templateCode.value); text.value = header + '\n' } finally { loadingTemplate.value = false }
}
function submit() { emit('import', parseAssetCsv(text.value), upsert.value) }
</script>
<template>
  <div class="modal-backdrop">
    <section class="asset-editor import-dialog">
      <header>
        <div><span>CSV 批量导入</span><h2>导入资产台账</h2></div>
        <button aria-label="关闭" @click="emit('close')"><X /></button>
      </header>
      <div class="import-tip"><UploadCloud /><p>首行 = 标准字段 + 自定义字段（如 bmc_ip / rack_no / u_position / serial）。标签用 <code>|</code> 分隔；勾选“按编号/IP 更新”则重复资产直接更新。</p></div>
      <div class="import-controls">
        <label><span>模板模型</span><select v-model="templateCode"><option v-for="m in props.models ?? []" :key="m.code" :value="m.code">{{ m.name }}</option></select></label>
        <button type="button" :disabled="loadingTemplate" @click="loadTemplate"><Download /> 载入该模型模板</button>
        <label class="upsert"><input v-model="upsert" type="checkbox"> 重复时按 编号/IP 更新（upsert）</label>
      </div>
      <textarea v-model="text" rows="10"></textarea>
      <div v-if="result" class="import-result">成功 {{ result.created }} / 更新 {{ result.updated || 0 }} / 共 {{ result.total }}<span v-for="item in result.errors" :key="item.row">第 {{ item.row }} 行：{{ item.message }}</span></div>
      <footer>
        <button @click="emit('close')">取消</button>
        <button class="primary" @click="submit">开始导入</button>
      </footer>
    </section>
  </div>
</template>
