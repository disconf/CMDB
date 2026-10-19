<template>
  <section class="knowledge-center">
    <header class="kc-header">
      <div>
        <span>KNOWLEDGE CENTER</span>
        <h1>运维知识库</h1>
        <p>多空间、目录化、权限可控的运维知识沉淀与复用。</p>
      </div>
      <div class="kc-header-actions">
        <button v-if="canManage" @click="openSpaceDialog"><Plus/>新建空间</button>
        <button v-if="canManage" @click="openFolderDialog()"><FolderPlus/>新建目录</button>
        <button class="primary" @click="newDoc"><FilePlus2/>新建文档</button>
        <button @click="boot"><RefreshCw :class="{ spin: loading }"/>刷新</button>
      </div>
    </header>

    <p v-if="error" class="kc-error">{{ error }}</p>
    <p v-if="message" class="kc-message">{{ message }}</p>

    <div class="kc-layout">
      <aside class="kc-sidebar">
        <div class="space-picker">
          <label>知识空间</label>
          <select :value="selectedSpaceId" @change="selectSpace(($event.target as HTMLSelectElement).value)">
            <option v-for="space in spaces" :key="space.id" :value="space.id">
              {{ space.name }} · {{ space.documentCount }} 篇
            </option>
          </select>
          <small>{{ selectedSpace?.description || '选择知识空间开始浏览' }}</small>
        </div>
        <div class="tree-heading">
          <span>目录</span>
          <button @click="selectFolder('')" :class="{ active: !selectedFolderId }">全部文档</button>
        </div>
        <div class="folder-tree">
          <button
            v-for="folder in flattenedFolders"
            :key="folder.id"
            class="folder-row"
            :class="{ active: selectedFolderId === folder.id }"
            :style="{ paddingLeft: 10 + folder.depth * 16 + 'px' }"
            @click="selectFolder(folder.id)"
          >
            <span class="folder-toggle" @click.stop="toggleFolder(folder.id)">
              <ChevronDown v-if="folder.children?.length && expanded[folder.id]" />
              <ChevronRight v-else-if="folder.children?.length" />
              <span v-else class="folder-dot"></span>
            </span>
            <Folder />
            <span class="folder-name">{{ folder.name }}</span>
            <b>{{ folder.documentCount }}</b>
          </button>
          <p v-if="!allFolders.length" class="tree-empty">当前空间还没有目录</p>
        </div>
        <div class="sidebar-footer">
          <button @click="openSpaceDialog" v-if="canManage"><Plus/>空间管理</button>
          <button @click="openFolderDialog()" v-if="canManage"><FolderPlus/>目录</button>
        </div>
      </aside>

      <main class="kc-main">
        <template v-if="view !== 'edit'">
          <div class="kc-searchbar">
            <label><Search/><input v-model="query" placeholder="搜索标题、正文、标签" @keyup.enter="loadDocs"></label>
            <select v-model="statusFilter" @change="loadDocs">
              <option value="">全部状态</option>
              <option value="published">已发布</option>
              <option value="draft">草稿</option>
              <option value="archived">已归档</option>
            </select>
            <button @click="loadDocs">检索</button>
            <button @click="ragOpen = !ragOpen"><Sparkles/>知识问答</button>
          </div>

          <section v-if="ragOpen" class="rag-panel">
            <header><b>知识库问答</b><span>只引用当前有权查看的已发布知识</span></header>
            <div><input v-model="question" placeholder="例如：磁盘空间不足应该怎么排查？" @keyup.enter="ask"><button :disabled="asking || !question.trim()" @click="ask">{{ asking ? '生成中' : '提问' }}</button></div>
            <article v-if="answer"><p>{{ answer.answer }}</p><button v-for="item in answer.citations" :key="item.knowledgeId" class="citation" @click="openDoc(item.knowledgeId)"><b>{{ item.title }}</b><span>{{ item.category }} · 相关度 {{ Number(item.score).toFixed(2) }}</span></button></article>
          </section>

          <div class="doc-toolbar">
            <div><b>{{ currentFolderName }}</b><span>{{ documents.length }} 篇文档</span></div>
            <div v-if="selectedSpace"><span class="visibility-pill">{{ visibilityLabel(selectedSpace.visibility) }}</span><span class="visibility-pill">{{ kindLabel(selectedSpace.kind) }}</span></div>
          </div>

          <div class="doc-layout" :class="{ 'with-detail': view === 'detail' && selected }">
            <div class="doc-list">
              <article v-for="doc in documents" :key="doc.id" :class="{ active: selected?.id === doc.id }" @click="openDoc(doc.id)">
                <div class="doc-top"><span class="status-pill" :data-status="doc.status">{{ statusLabel(doc.status) }}</span><span>{{ doc.updatedAt }}</span></div>
                <h3>{{ doc.title }}</h3>
                <p>{{ doc.summary }}</p>
                <div class="doc-tags"><span v-for="tag in doc.tags" :key="tag">{{ tag }}</span></div>
                <footer><span>{{ doc.owner || doc.author || '未指定负责人' }}</span><span>{{ doc.views }} 浏览</span><span>v{{ doc.version }}</span></footer>
              </article>
              <div v-if="!documents.length && !loading" class="doc-empty"><BookOpen/><strong>当前目录没有文档</strong><span>新建文档或切换目录查看。</span></div>
            </div>

            <aside v-if="view === 'detail' && selected" class="doc-detail">
              <header>
                <div><span>{{ folderName(selected.folderId) }} · {{ statusLabel(selected.status) }}</span><h2>{{ selected.title }}</h2><p>{{ selected.summary }}</p></div>
                <button aria-label="关闭" @click="view = 'list'"><X/></button>
              </header>
              <div class="doc-meta">
                <span>负责人 {{ selected.owner || selected.author || '未指定' }}</span>
                <span>更新 {{ selected.updatedAt }}</span>
                <span>版本 v{{ selected.version }}</span>
                <span>{{ selected.views }} 次浏览</span>
              </div>
              <div class="doc-content markdown" v-html="renderContent(selected)"></div>
              <div class="doc-tags"><span v-for="tag in selected.tags" :key="tag">{{ tag }}</span></div>
              <div class="detail-actions">
                <button @click="editDoc"><Pencil/>编辑</button>
                <button v-if="canPublishSelected" @click="publishSelected">发布</button>
                <button @click="openPermissions('document', selected.id)"><ShieldCheck/>权限</button>
                <button @click="openVersions"><History/>版本</button>
                <button v-if="canManage" @click="archiveSelected">归档</button>
                <button v-if="canManage" class="danger" @click="deleteSelected">删除</button>
              </div>
              <section class="attachment-panel">
                <header><b>附件</b><label v-if="canEditSelected" class="upload-label">{{ uploading ? '上传中' : '上传附件' }}<input type="file" :disabled="uploading" @change="uploadAttachment"></label></header>
                <article v-for="item in attachments" :key="item.id"><button @click="downloadAttachment(item)"><b>{{ item.filename }}</b><span>{{ (item.size / 1024).toFixed(1) }} KB · {{ item.uploadedBy }}</span></button><button v-if="canManage" class="danger" @click="deleteAttachment(item)">删除</button></article>
                <p v-if="!attachments.length">暂无附件</p>
              </section>
              <section v-if="showVersions" class="version-panel">
                <header><b>版本历史</b><button @click="showVersions = false">关闭</button></header>
                <article v-for="item in versions" :key="item.id"><b>v{{ item.version }}</b><span>{{ item.changeSummary || '知识更新' }} · {{ item.changedBy }} · {{ item.createdAt }}</span><button v-if="canManage" @click="restoreVersion(item.version)">还原</button></article>
              </section>
            </aside>
          </div>
        </template>

        <template v-else>
          <header class="editor-header">
            <div><span>{{ editorMode === 'create' ? 'NEW DOCUMENT' : 'EDIT DOCUMENT' }}</span><h2>{{ editorMode === 'create' ? '新建文档' : '编辑文档' }}</h2></div>
            <button @click="cancelEdit"><ArrowLeft/>返回</button>
          </header>
          <div class="editor-form">
            <label class="wide">标题<input v-model="form.title" placeholder="文档标题" required></label>
            <label>目录<select v-model="form.folderId"><option v-for="folder in folderOptions" :key="folder.id" :value="folder.id">{{ '　'.repeat(folder.depth) + folder.name }}</option></select></label>
            <label>状态<select v-model="form.status"><option value="draft">草稿</option><option value="published">发布</option></select></label>
            <label>可见范围<select v-model="form.visibility"><option value="organization">组织内</option><option value="project">项目组</option><option value="private">私有</option></select></label>
            <label>负责人<input v-model="form.owner" placeholder="默认当前用户"></label>
            <label class="wide">摘要<textarea v-model="form.summary" placeholder="一句话说明文档用途"></textarea></label>
            <label class="wide">标签<input v-model="form.tags" placeholder="用英文逗号分隔"></label>
            <div class="wide">
              <label>正文</label>
              <KnowledgeRichEditor v-model="form.content" :format="form.contentFormat" @update:format="form.contentFormat = $event" />
            </div>
          </div>
          <footer class="editor-footer">
            <button @click="cancelEdit">取消</button>
            <button @click="saveDoc('draft')" :disabled="saving">保存草稿</button>
            <button class="primary" @click="saveDoc('published')" :disabled="saving">{{ saving ? '保存中' : '保存并发布' }}</button>
          </footer>
        </template>
      </main>
    </div>

    <KnowledgePermissionDialog
      :open="permissionDialog.open"
      :resource-type="permissionDialog.resourceType"
      :resource-id="permissionDialog.resourceId"
      :token="auth.token"
      @close="permissionDialog.open = false"
      @saved="permissionDialog.open = false"
    />

    <div v-if="spaceDialog.open" class="kc-modal" @click.self="spaceDialog.open = false">
      <form class="kc-dialog" @submit.prevent="saveSpace">
        <header><div><span>KNOWLEDGE SPACE</span><h3>新建知识空间</h3></div><button type="button" @click="spaceDialog.open = false"><X/></button></header>
        <div class="dialog-grid">
          <label>名称<input v-model="spaceDialog.name" required></label>
          <label>编码<input v-model="spaceDialog.code" placeholder="可留空自动生成"></label>
          <label>类型<select v-model="spaceDialog.kind"><option value="organization">组织知识空间</option><option value="project">项目组知识空间</option></select></label>
          <label>可见范围<select v-model="spaceDialog.visibility"><option value="organization">组织内</option><option value="project">项目组</option></select></label>
          <label class="wide">负责人<input v-model="spaceDialog.owner" placeholder="默认当前用户"></label>
          <label class="wide">说明<textarea v-model="spaceDialog.description"></textarea></label>
        </div>
        <footer><button type="button" @click="spaceDialog.open = false">取消</button><button class="primary" type="submit">创建空间</button></footer>
      </form>
    </div>

    <div v-if="folderDialog.open" class="kc-modal" @click.self="folderDialog.open = false">
      <form class="kc-dialog" @submit.prevent="saveFolder">
        <header><div><span>KNOWLEDGE FOLDER</span><h3>{{ folderDialog.id ? '编辑目录' : '新建目录' }}</h3></div><button type="button" @click="folderDialog.open = false"><X/></button></header>
        <div class="dialog-grid">
          <label>名称<input v-model="folderDialog.name" required></label>
          <label>上级目录<select v-model="folderDialog.parentId"><option value="">根目录</option><option v-for="folder in folderOptions.filter(item => item.id !== folderDialog.id)" :key="folder.id" :value="folder.id">{{ '　'.repeat(folder.depth) + folder.name }}</option></select></label>
          <label>负责人<input v-model="folderDialog.owner"></label>
          <label>排序<input v-model.number="folderDialog.sortOrder" type="number"></label>
          <label class="wide">说明<textarea v-model="folderDialog.description"></textarea></label>
        </div>
        <footer>
          <button v-if="folderDialog.id" type="button" class="danger" @click="removeFolder">删除</button>
          <span class="dialog-spacer"></span>
          <button type="button" @click="folderDialog.open = false">取消</button>
          <button class="primary" type="submit">保存</button>
        </footer>
      </form>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import {
  ArrowLeft, BookOpen, ChevronDown, ChevronRight, FilePlus2, Folder, FolderPlus,
  History, Pencil, Plus, RefreshCw, Search, ShieldCheck, Sparkles, X,
} from 'lucide-vue-next'
import { useAuthStore } from '@/stores/useAuthStore'
import * as knowledgeApi from '@/api/knowledge'
import type { KnowledgeDoc, KnowledgeFolder, KnowledgeFormat, KnowledgeSpace, KnowledgeTree } from '@/api/knowledge'
import KnowledgeRichEditor from './KnowledgeRichEditor.vue'
import KnowledgePermissionDialog from './KnowledgePermissionDialog.vue'

const auth = useAuthStore()
const spaces = ref<KnowledgeSpace[]>([])
const tree = ref<KnowledgeTree>()
const selectedSpaceId = ref('')
const selectedFolderId = ref('')
const documents = ref<KnowledgeDoc[]>([])
const selected = ref<KnowledgeDoc>()
const view = ref<'list' | 'detail' | 'edit'>('list')
const editorMode = ref<'create' | 'edit'>('create')
const loading = ref(false)
const saving = ref(false)
const uploading = ref(false)
const error = ref('')
const message = ref('')
const query = ref('')
const statusFilter = ref('')
const expanded = ref<Record<string, boolean>>({})
const versions = ref<Array<{ id: string; version: number; changeSummary: string; changedBy: string; createdAt: string }>>([])
const attachments = ref<Array<{ id: string; filename: string; size: number; uploadedBy: string }>>([])
const showVersions = ref(false)
const ragOpen = ref(false)
const question = ref('')
const answer = ref<{ answer: string; citations: Array<{ knowledgeId: string; title: string; category: string; score: number }> }>()
const asking = ref(false)

const form = reactive({
  id: '', spaceId: '', folderId: '', title: '', category: '', summary: '', content: '',
  contentFormat: 'markdown' as KnowledgeFormat, tags: '', status: 'draft', owner: '', visibility: 'organization', version: 0,
})
const spaceDialog = reactive({ open: false, name: '', code: '', kind: 'organization', visibility: 'organization', owner: '', description: '' })
const folderDialog = reactive({ open: false, id: '', name: '', parentId: '', description: '', owner: '', sortOrder: 0, originalParentId: '' })
const permissionDialog = reactive<{ open: boolean; resourceType: 'folder' | 'document'; resourceId: string }>({
  open: false, resourceType: 'document', resourceId: '',
})

const selectedSpace = computed(() => spaces.value.find((space) => space.id === selectedSpaceId.value))
const allFolders = computed(() => flattenFolders(tree.value?.folders || []))
const flattenedFolders = computed(() => flattenFolders(tree.value?.folders || [], true))
const folderOptions = computed(() => allFolders.value)
const currentFolderName = computed(() => selectedFolderId.value ? folderName(selectedFolderId.value) : '全部文档')
const canManage = computed(() => auth.user?.permissions.some((permission) => ['aiops:execute', 'knowledge:manage', 'knowledge:manage_acl', 'knowledge:manage_folder'].includes(permission)) ?? false)
const canEditSelected = computed(() => Boolean(selected.value && (canManage.value || selected.value.owner === auth.user?.username || selected.value.author === auth.user?.username)))
const canPublishSelected = computed(() => canEditSelected.value)

function flattenFolders(items: KnowledgeFolder[], visibleOnly = false, depth = 0): Array<KnowledgeFolder & { depth: number }> {
  const result: Array<KnowledgeFolder & { depth: number }> = []
  for (const folder of items) {
    result.push({ ...folder, depth })
    if (!visibleOnly || expanded.value[folder.id]) {
      result.push(...flattenFolders(folder.children || [], visibleOnly, depth + 1))
    }
  }
  return result
}

function toggleFolder(id: string) {
  expanded.value = { ...expanded.value, [id]: !expanded.value[id] }
}

function headers() {
  return { Authorization: `Bearer ${auth.token}` }
}

async function boot() {
  loading.value = true
  error.value = ''
  try {
    spaces.value = await knowledgeApi.fetchKnowledgeSpaces(auth.token)
    if (!spaces.value.length) {
      error.value = '当前账号没有可访问的知识空间'
      return
    }
    const preferred = spaces.value.find((space) => !space.private) || spaces.value[0]
    selectedSpaceId.value = preferred.id
    await loadTree()
    await loadDocs()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '知识库加载失败'
  } finally {
    loading.value = false
  }
}

async function loadTree() {
  if (!selectedSpaceId.value) return
  tree.value = await knowledgeApi.fetchKnowledgeTree(auth.token, selectedSpaceId.value)
  spaces.value = tree.value.spaces
  const roots = flattenFolders(tree.value.folders)
  for (const folder of roots) {
    if (!(folder.id in expanded.value)) expanded.value[folder.id] = true
  }
  if (selectedFolderId.value && !allFolders.value.some((folder) => folder.id === selectedFolderId.value)) {
    selectedFolderId.value = ''
  }
}

async function loadDocs() {
  if (!selectedSpaceId.value) return
  loading.value = true
  error.value = ''
  try {
    documents.value = await knowledgeApi.fetchKnowledgeDocuments(auth.token, {
      spaceId: selectedSpaceId.value,
      folderId: selectedFolderId.value,
      q: query.value,
      status: statusFilter.value,
      includeSubfolders: true,
    })
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '文档加载失败'
  } finally {
    loading.value = false
  }
}

async function selectSpace(id: string) {
  selectedSpaceId.value = id
  selectedFolderId.value = ''
  view.value = 'list'
  selected.value = undefined
  await loadTree()
  await loadDocs()
}

async function selectFolder(id: string) {
  selectedFolderId.value = id
  view.value = 'list'
  selected.value = undefined
  await loadDocs()
}

async function openDoc(id: string) {
  try {
    selected.value = await knowledgeApi.fetchKnowledgeDocument(auth.token, id)
    view.value = 'detail'
    showVersions.value = false
    versions.value = []
    await loadAttachments()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '文档加载失败'
  }
}

function newDoc() {
  if (!selectedSpaceId.value) return
  const folder = selectedFolderId.value || allFolders.value[0]?.id || ''
  if (!folder) {
    error.value = '请先在当前空间创建目录'
    return
  }
  Object.assign(form, {
    id: '', spaceId: selectedSpaceId.value, folderId: folder, title: '', category: folderName(folder), summary: '',
    content: '', contentFormat: 'markdown', tags: '', status: 'draft', owner: auth.user?.username || '', visibility: selectedSpace.value?.visibility || 'organization', version: 0,
  })
  editorMode.value = 'create'
  view.value = 'edit'
  error.value = ''
  message.value = ''
}

function editDoc() {
  if (!selected.value) return
  Object.assign(form, {
    id: selected.value.id, spaceId: selected.value.spaceId || selectedSpaceId.value, folderId: selected.value.folderId || selectedFolderId.value,
    title: selected.value.title, category: selected.value.category, summary: selected.value.summary, content: selected.value.content,
    contentFormat: selected.value.contentFormat || 'markdown', tags: selected.value.tags.join(','), status: selected.value.status,
    owner: selected.value.owner || selected.value.author || '', visibility: selected.value.visibility || 'organization', version: selected.value.version,
  })
  editorMode.value = 'edit'
  view.value = 'edit'
}

function cancelEdit() {
  view.value = selected.value ? 'detail' : 'list'
}

async function saveDoc(status: string) {
  if (!form.title.trim() || !form.summary.trim() || !form.content.trim()) {
    error.value = '请填写标题、摘要和正文'
    return
  }
  saving.value = true
  error.value = ''
  message.value = ''
  const payload: knowledgeApi.KnowledgePayload = {
    spaceId: form.spaceId,
    folderId: form.folderId,
    title: form.title.trim(),
    category: form.category || folderName(form.folderId),
    summary: form.summary.trim(),
    content: form.content,
    contentFormat: form.contentFormat,
    tags: form.tags.split(',').map((item) => item.trim()).filter(Boolean),
    status,
    owner: form.owner.trim() || auth.user?.username,
    visibility: form.visibility,
    version: form.version,
  }
  try {
    const item = editorMode.value === 'create'
      ? await knowledgeApi.createKnowledgeDocument(auth.token, payload)
      : await knowledgeApi.updateKnowledgeDocument(auth.token, form.id, payload)
    selected.value = item
    message.value = status === 'published' ? '文档已发布' : '草稿已保存'
    view.value = 'detail'
    await loadTree()
    await loadDocs()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '保存失败'
  } finally {
    saving.value = false
  }
}

async function publishSelected() {
  if (!selected.value) return
  try {
    selected.value = await knowledgeApi.publishKnowledgeDocument(auth.token, selected.value.id)
    message.value = '文档已发布'
    await loadDocs()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '发布失败'
  }
}

async function archiveSelected() {
  if (!selected.value || !window.confirm('确认归档该文档？')) return
  try {
    selected.value = await knowledgeApi.archiveKnowledgeDocument(auth.token, selected.value.id)
    message.value = '文档已归档'
    view.value = 'list'
    await loadDocs()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '归档失败'
  }
}

async function deleteSelected() {
  if (!selected.value || !window.confirm(`确认删除文档“${selected.value.title}”？`)) return
  try {
    await knowledgeApi.deleteKnowledgeDocument(auth.token, selected.value.id)
    selected.value = undefined
    view.value = 'list'
    message.value = '文档已删除'
    await loadTree()
    await loadDocs()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '删除失败'
  }
}

function openPermissions(resourceType: 'folder' | 'document', resourceId: string) {
  permissionDialog.open = true
  permissionDialog.resourceType = resourceType
  permissionDialog.resourceId = resourceId
}

function openSpaceDialog() {
  Object.assign(spaceDialog, { open: true, name: '', code: '', kind: 'organization', visibility: 'organization', owner: auth.user?.username || '', description: '' })
}

async function saveSpace() {
  try {
    await knowledgeApi.createKnowledgeSpace(auth.token, {
      name: spaceDialog.name, code: spaceDialog.code, kind: spaceDialog.kind,
      visibility: spaceDialog.visibility, owner: spaceDialog.owner, description: spaceDialog.description,
    })
    spaceDialog.open = false
    message.value = '知识空间已创建'
    await boot()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '空间创建失败'
  }
}

function openFolderDialog(folder?: KnowledgeFolder) {
  if (folder) {
    Object.assign(folderDialog, { open: true, id: folder.id, name: folder.name, parentId: folder.parentId || '', description: folder.description, owner: folder.owner, sortOrder: folder.sortOrder, originalParentId: folder.parentId || '' })
  } else {
    Object.assign(folderDialog, { open: true, id: '', name: '', parentId: selectedFolderId.value, description: '', owner: auth.user?.username || '', sortOrder: 0, originalParentId: '' })
  }
}

async function saveFolder() {
  try {
    if (folderDialog.id) {
      await knowledgeApi.updateKnowledgeFolder(auth.token, folderDialog.id, {
        spaceId: selectedSpaceId.value, parentId: folderDialog.parentId, name: folderDialog.name,
        description: folderDialog.description, owner: folderDialog.owner, sortOrder: folderDialog.sortOrder,
      })
      if (folderDialog.parentId !== folderDialog.originalParentId) {
        await knowledgeApi.moveKnowledgeFolder(auth.token, folderDialog.id, folderDialog.parentId)
      }
    } else {
      await knowledgeApi.createKnowledgeFolder(auth.token, {
        spaceId: selectedSpaceId.value, parentId: folderDialog.parentId, name: folderDialog.name,
        description: folderDialog.description, owner: folderDialog.owner, sortOrder: folderDialog.sortOrder,
      })
    }
    folderDialog.open = false
    message.value = '目录已保存'
    await loadTree()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '目录保存失败'
  }
}

async function removeFolder() {
  if (!folderDialog.id || !window.confirm(`确认删除目录“${folderDialog.name}”？`)) return
  try {
    await knowledgeApi.deleteKnowledgeFolder(auth.token, folderDialog.id)
    folderDialog.open = false
    selectedFolderId.value = ''
    message.value = '目录已删除'
    await loadTree()
    await loadDocs()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '目录删除失败'
  }
}

async function loadVersions() {
  if (!selected.value) return
  try {
    const response = await fetch(`/api/v1/aiops/knowledge/${encodeURIComponent(selected.value.id)}/versions`, { headers: headers() })
    if (!response.ok) throw new Error('版本历史加载失败')
    versions.value = await response.json()
    showVersions.value = true
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '版本历史加载失败'
  }
}

async function openVersions() {
  await loadVersions()
}

async function restoreVersion(version: number) {
  if (!selected.value || !window.confirm(`确认还原到版本 v${version}？`)) return
  try {
    const response = await fetch(`/api/v1/aiops/knowledge/${encodeURIComponent(selected.value.id)}/versions/${version}/restore`, { method: 'POST', headers: headers() })
    if (!response.ok) throw new Error('版本还原失败')
    selected.value = await response.json()
    message.value = `已还原到版本 v${version}`
    await loadVersions()
    await loadDocs()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '版本还原失败'
  }
}

async function loadAttachments() {
  if (!selected.value) return
  try {
    const response = await fetch(`/api/v1/aiops/knowledge/${encodeURIComponent(selected.value.id)}/attachments`, { headers: headers() })
    attachments.value = response.ok ? await response.json() : []
  } catch {
    attachments.value = []
  }
}

async function uploadAttachment(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file || !selected.value) return
  uploading.value = true
  try {
    const data = new FormData()
    data.append('file', file)
    const response = await fetch(`/api/v1/aiops/knowledge/${encodeURIComponent(selected.value.id)}/attachments`, { method: 'POST', headers: headers(), body: data })
    if (!response.ok) throw new Error('附件上传失败')
    await loadAttachments()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '附件上传失败'
  } finally {
    uploading.value = false
    input.value = ''
  }
}

async function downloadAttachment(item: { id: string; filename: string }) {
  if (!selected.value) return
  const response = await fetch(`/api/v1/aiops/knowledge/${encodeURIComponent(selected.value.id)}/attachments/${encodeURIComponent(item.id)}`, { headers: headers() })
  if (!response.ok) {
    error.value = '附件下载失败'
    return
  }
  const blob = await response.blob()
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = item.filename
  link.click()
  URL.revokeObjectURL(url)
}

async function deleteAttachment(item: { id: string }) {
  if (!selected.value || !window.confirm('确认删除该附件？')) return
  const response = await fetch(`/api/v1/aiops/knowledge/${encodeURIComponent(selected.value.id)}/attachments/${encodeURIComponent(item.id)}`, { method: 'DELETE', headers: headers() })
  if (!response.ok) {
    error.value = '附件删除失败'
    return
  }
  await loadAttachments()
}

async function ask() {
  if (!question.value.trim()) return
  asking.value = true
  try {
    const response = await fetch('/api/v1/aiops/knowledge/ask', {
      method: 'POST', headers: { ...headers(), 'Content-Type': 'application/json' },
      body: JSON.stringify({ question: question.value, limit: 5 }),
    })
    const data = await response.json().catch(() => ({}))
    if (!response.ok) throw new Error(data.message || '知识问答失败')
    answer.value = data
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '知识问答失败'
  } finally {
    asking.value = false
  }
}

function folderName(id?: string) {
  if (!id) return '未分类'
  return allFolders.value.find((folder) => folder.id === id)?.name || '未分类'
}

function statusLabel(status: string) {
  return ({ published: '已发布', draft: '草稿', archived: '已归档' } as Record<string, string>)[status] || status
}

function visibilityLabel(value: string) {
  return ({ organization: '组织内', project: '项目组', private: '私有' } as Record<string, string>)[value] || value
}

function kindLabel(value: string) {
  return ({ organization: '组织空间', project: '项目空间', private: '私有空间' } as Record<string, string>)[value] || value
}

function escapeHTML(value: string) {
  return value.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;')
}

function renderMarkdown(value: string) {
  let html = escapeHTML(value || '')
  html = html.replace(/```([\s\S]*?)```/g, '<pre><code>$1</code></pre>')
  html = html.replace(/^### (.*)$/gm, '<h3>$1</h3>')
  html = html.replace(/^## (.*)$/gm, '<h2>$1</h2>')
  html = html.replace(/^# (.*)$/gm, '<h1>$1</h1>')
  html = html.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')
  html = html.replace(/`([^`]+)`/g, '<code>$1</code>')
  html = html.replace(/^- (.*)$/gm, '<li>$1</li>')
  html = html.replace(/\n/g, '<br>')
  return html
}

function sanitizeHTML(value: string) {
  const allowed = new Set(['H1', 'H2', 'H3', 'P', 'BR', 'STRONG', 'B', 'EM', 'I', 'U', 'UL', 'OL', 'LI', 'CODE', 'PRE', 'BLOCKQUOTE', 'A', 'SPAN', 'DIV'])
  const container = document.createElement('div')
  container.innerHTML = value || ''
  const walk = (node: Node) => {
    for (const child of Array.from(node.childNodes)) {
      if (child.nodeType === Node.ELEMENT_NODE) {
        const element = child as HTMLElement
        if (!allowed.has(element.tagName)) {
          element.replaceWith(document.createTextNode(element.textContent || ''))
          continue
        }
        for (const attribute of Array.from(element.attributes)) {
          if (element.tagName === 'A' && attribute.name === 'href' && /^https?:\/\//i.test(attribute.value)) continue
          element.removeAttribute(attribute.name)
        }
        walk(element)
      }
    }
  }
  walk(container)
  return container.innerHTML
}

function renderContent(doc: KnowledgeDoc) {
  return doc.contentFormat === 'html' ? sanitizeHTML(doc.content) : renderMarkdown(doc.content)
}

onMounted(boot)
</script>

<style scoped>
.knowledge-center { color: #dcecff; }
.kc-header { display: flex; justify-content: space-between; gap: 18px; align-items: flex-start; }
.kc-header > div:first-child > span, .editor-header span { color: #22d3ee; font-size: 10px; letter-spacing: .16em; }
.kc-header h1 { margin: 6px 0; font-size: 26px; }
.kc-header p { margin: 0; color: #718ba2; font-size: 12px; }
.kc-header-actions { display: flex; flex-wrap: wrap; gap: 8px; }
.kc-header-actions button, .kc-sidebar button, .kc-searchbar button, .kc-main button, .kc-dialog button, .doc-detail button, .editor-footer button { display: inline-flex; align-items: center; gap: 6px; border: 1px solid #21445d; background: #0b263b; color: #bdd8e9; padding: 8px 11px; cursor: pointer; }
.kc-header-actions button.primary, .kc-main button.primary, .kc-dialog button.primary, .editor-footer button.primary { background: #087c9e; border-color: #087c9e; color: #fff; }
.kc-header-actions button.danger, .kc-main button.danger, .kc-dialog button.danger, .doc-detail button.danger { border-color: #783e49; color: #ff9aa7; }
.kc-header-actions svg, .kc-sidebar svg, .kc-main svg { width: 14px; height: 14px; }
.kc-error, .kc-message { margin: 12px 0; padding: 9px 11px; font-size: 12px; }
.kc-error { border: 1px solid #783e49; background: #35161e; color: #ff9aa7; }
.kc-message { border: 1px solid #155e75; background: #082b38; color: #a5f3fc; }
.kc-layout { display: grid; grid-template-columns: 272px minmax(0, 1fr); gap: 14px; margin-top: 16px; }
.kc-sidebar { min-height: 640px; border: 1px solid #173b55; background: #071b2c; padding: 12px; display: flex; flex-direction: column; }
.space-picker { display: grid; gap: 6px; padding-bottom: 12px; border-bottom: 1px solid #173b55; }
.space-picker label { color: #7891a4; font-size: 10px; }
.space-picker select { height: 36px; border: 1px solid #21445d; background: #061725; color: #dcecff; padding: 0 8px; }
.space-picker small { color: #617e92; font-size: 10px; }
.tree-heading { display: flex; justify-content: space-between; align-items: center; margin: 12px 0 8px; color: #7891a4; font-size: 10px; }
.tree-heading button { padding: 4px 7px !important; font-size: 10px; }
.folder-tree { display: grid; gap: 3px; overflow: auto; }
.folder-row { width: 100%; display: flex; align-items: center; gap: 6px; padding: 7px 8px !important; border: 1px solid transparent !important; text-align: left; font-size: 12px; }
.folder-row.active { border-color: #16b8e4 !important; background: #0c4967; }
.folder-toggle { width: 12px; display: inline-flex; color: #6f91a5; }
.folder-dot { width: 4px; height: 4px; border-radius: 50%; background: #31556b; }
.folder-name { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.folder-row b { color: #6f91a5; font-size: 10px; font-weight: 400; }
.tree-empty { padding: 18px 6px; color: #617e92; font-size: 11px; }
.sidebar-footer { display: flex; gap: 6px; margin-top: auto; padding-top: 12px; border-top: 1px solid #173b55; }
.sidebar-footer button { flex: 1; justify-content: center; font-size: 10px; }
.kc-main { min-width: 0; }
.kc-searchbar { display: flex; gap: 8px; }
.kc-searchbar label { display: flex; align-items: center; flex: 1; border: 1px solid #21445d; background: #071b2c; padding: 0 10px; }
.kc-searchbar label svg { color: #22d3ee; }
.kc-searchbar input { width: 100%; height: 36px; border: 0; outline: 0; background: transparent; color: #dcecff; }
.kc-searchbar select { height: 36px; border: 1px solid #21445d; background: #071b2c; color: #bdd8e9; padding: 0 8px; }
.rag-panel { margin-top: 12px; border: 1px solid #1b4b62; background: #0a273b; padding: 12px; }
.rag-panel header { display: flex; justify-content: space-between; color: #22d3ee; font-size: 10px; }
.rag-panel > div { display: flex; gap: 8px; margin-top: 9px; }
.rag-panel input { flex: 1; height: 34px; border: 1px solid #21445d; background: #061725; color: #dcecff; padding: 0 8px; }
.rag-panel article { margin-top: 10px; padding-top: 10px; border-top: 1px solid #173b55; color: #b8cedb; font-size: 12px; }
.citation { display: grid; gap: 2px; margin-top: 6px; text-align: left; }
.citation span { color: #617e92; font-size: 10px; }
.doc-toolbar { display: flex; justify-content: space-between; align-items: center; margin: 14px 0 9px; color: #7891a4; font-size: 11px; }
.doc-toolbar > div { display: flex; align-items: center; gap: 9px; }
.doc-toolbar b { color: #dcecff; font-size: 16px; }
.visibility-pill { padding: 3px 7px; border: 1px solid #1b4b62; color: #7fb1c5; font-size: 10px; }
.doc-layout { display: grid; grid-template-columns: 1fr; gap: 12px; }
.doc-layout.with-detail { grid-template-columns: minmax(300px, .9fr) minmax(420px, 1.1fr); }
.doc-list { display: grid; align-content: start; gap: 8px; max-height: 680px; overflow: auto; }
.doc-list article { padding: 13px; border: 1px solid #173b55; background: #0a263a; cursor: pointer; }
.doc-list article.active { border-color: #22d3ee; }
.doc-top, .doc-list footer, .doc-meta { display: flex; justify-content: space-between; gap: 8px; align-items: center; color: #617e92; font-size: 10px; }
.status-pill { padding: 3px 6px; background: #26303b; color: #94a3b8; }
.status-pill[data-status=published] { background: #0b3a3b; color: #5eead4; }
.status-pill[data-status=draft] { background: #3d3317; color: #fbbf24; }
.status-pill[data-status=archived] { background: #302b3d; color: #c4b5fd; }
.doc-list h3 { margin: 8px 0 6px; font-size: 15px; }
.doc-list p { margin: 0; color: #7891a4; font-size: 12px; line-height: 1.6; }
.doc-list footer { margin-top: 9px; }
.doc-tags { display: flex; flex-wrap: wrap; gap: 5px; margin-top: 9px; }
.doc-tags span { padding: 3px 6px; background: #10334a; color: #7fb1c5; font-size: 10px; }
.doc-empty { display: grid; place-items: center; gap: 6px; min-height: 260px; color: #617e92; text-align: center; }
.doc-empty svg { width: 30px; height: 30px; color: #22d3ee; }
.doc-detail { border: 1px solid #1b4b62; background: #071d2f; padding: 16px; max-height: 680px; overflow: auto; }
.doc-detail > header { display: flex; justify-content: space-between; gap: 8px; }
.doc-detail header span { color: #22d3ee; font-size: 10px; }
.doc-detail h2 { margin: 6px 0; }
.doc-detail header p { margin: 0; color: #7891a4; font-size: 12px; }
.doc-detail header button { border: 0; background: transparent; color: #8aa6b9; padding: 0; }
.doc-meta { margin: 12px 0; padding: 9px 0; border-top: 1px solid #15354d; border-bottom: 1px solid #15354d; }
.doc-content { color: #b8cedb; line-height: 1.75; font-size: 13px; }
.doc-content :deep(h1), .doc-content :deep(h2), .doc-content :deep(h3) { color: #e8f6ff; }
.doc-content :deep(code) { padding: 2px 5px; background: #0c2b3d; color: #7dd3fc; }
.doc-content :deep(pre) { padding: 12px; overflow: auto; background: #020b12; border: 1px solid #173b55; }
.detail-actions { display: flex; flex-wrap: wrap; gap: 7px; margin-top: 16px; }
.attachment-panel, .version-panel { margin-top: 14px; padding: 11px; border: 1px solid #15354d; background: #061620; }
.attachment-panel > header, .version-panel > header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 7px; }
.attachment-panel header b, .version-panel header b { color: #7891a4; font-size: 10px; }
.upload-label { padding: 5px 7px; border: 1px solid #21445d; color: #bdd8e9; font-size: 10px; cursor: pointer; }
.upload-label input { display: none; }
.attachment-panel article, .version-panel article { display: flex; align-items: center; gap: 8px; padding: 6px 0; border-top: 1px solid #123043; }
.attachment-panel article button:first-child { display: grid; flex: 1; text-align: left; }
.attachment-panel article span, .version-panel article span { color: #617e92; font-size: 10px; }
.attachment-panel p { color: #617e92; font-size: 10px; }
.editor-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
.editor-header h2 { margin: 5px 0; }
.editor-form { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; padding: 14px; border: 1px solid #173b55; background: #071b2c; }
.editor-form label { display: grid; gap: 5px; color: #7891a4; font-size: 11px; }
.editor-form label.wide, .editor-form > .wide { grid-column: 1 / -1; }
.editor-form input, .editor-form select, .editor-form textarea { border: 1px solid #22465f; background: #061725; color: #dcecff; padding: 8px; }
.editor-form textarea { min-height: 70px; }
.editor-footer { display: flex; justify-content: flex-end; gap: 8px; margin-top: 12px; }
.kc-modal { position: fixed; inset: 0; z-index: 70; display: grid; place-items: center; padding: 24px; background: rgba(2,8,14,.8); }
.kc-dialog { width: min(680px, 96vw); border: 1px solid #246c98; background: #071925; }
.kc-dialog > header { display: flex; justify-content: space-between; align-items: flex-start; padding: 14px 16px; border-bottom: 1px solid #173b55; }
.kc-dialog header span { color: #22d3ee; font-size: 10px; }
.kc-dialog h3 { margin: 5px 0; }
.kc-dialog header button { border: 0; background: transparent; color: #8aa6b9; padding: 0; }
.dialog-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; padding: 16px; }
.dialog-grid label { display: grid; gap: 5px; color: #7891a4; font-size: 11px; }
.dialog-grid label.wide { grid-column: 1 / -1; }
.dialog-grid input, .dialog-grid select, .dialog-grid textarea { border: 1px solid #22465f; background: #061725; color: #dcecff; padding: 8px; }
.kc-dialog > footer { display: flex; justify-content: flex-end; align-items: center; gap: 8px; padding: 12px 16px; border-top: 1px solid #173b55; }
.dialog-spacer { flex: 1; }
.spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 1100px) { .kc-layout { grid-template-columns: 230px minmax(0, 1fr); } .doc-layout.with-detail { grid-template-columns: 1fr; } }
@media (max-width: 820px) { .kc-layout { grid-template-columns: 1fr; } .kc-sidebar { min-height: auto; } .kc-header { flex-direction: column; } .editor-form, .dialog-grid { grid-template-columns: 1fr; } }
</style>
