<template>
  <div class="knowledge-rich-editor">
    <div class="editor-toolbar">
      <div class="mode-switch">
        <button type="button" :class="{ active: localFormat === 'markdown' }" @click="switchFormat('markdown')">Markdown</button>
        <button type="button" :class="{ active: localFormat === 'html' }" @click="switchFormat('html')">富文本</button>
      </div>
      <div v-if="localFormat === 'markdown'" class="editor-tools">
        <button type="button" title="标题" @click="wrapMarkdown('## ', '')">H2</button>
        <button type="button" title="粗体" @click="wrapMarkdown('**', '**')"><Bold/></button>
        <button type="button" title="斜体" @click="wrapMarkdown('_', '_')"><Italic/></button>
        <button type="button" title="代码" @click="wrapMarkdown('`', '`')"><Code2/></button>
        <button type="button" title="链接" @click="insertMarkdownLink"><Link2/></button>
        <button type="button" title="列表" @click="insertLinePrefix('- ')"><List/></button>
        <button type="button" title="引用" @click="insertLinePrefix('> ')">“”</button>
      </div>
      <div v-else class="editor-tools">
        <button type="button" title="粗体" @click="exec('bold')"><Bold/></button>
        <button type="button" title="斜体" @click="exec('italic')"><Italic/></button>
        <button type="button" title="下划线" @click="exec('underline')"><Underline/></button>
        <button type="button" title="二级标题" @click="execBlock('H2')">H2</button>
        <button type="button" title="三级标题" @click="execBlock('H3')">H3</button>
        <button type="button" title="无序列表" @click="exec('insertUnorderedList')"><List/></button>
        <button type="button" title="有序列表" @click="exec('insertOrderedList')"><ListOrdered/></button>
        <button type="button" title="引用" @click="execBlock('BLOCKQUOTE')">“”</button>
        <button type="button" title="代码" @click="insertRichCode"><Code2/></button>
        <button type="button" title="链接" @click="insertRichLink"><Link2/></button>
      </div>
    </div>

    <div class="editor-surface" :class="{ split: localFormat === 'markdown' }">
      <textarea
        v-if="localFormat === 'markdown'"
        ref="textarea"
        v-model="localContent"
        class="markdown-input"
        :placeholder="placeholder"
        @input="emitContent"
      ></textarea>
      <div
        v-else
        ref="rich"
        class="rich-input"
        contenteditable="true"
        :data-placeholder="placeholder"
        @input="onRichInput"
        @paste="onPaste"
      ></div>
      <div v-if="localFormat === 'markdown'" class="markdown-preview markdown" v-html="renderedMarkdown"></div>
    </div>
    <p class="editor-hint">
      {{ localFormat === 'markdown' ? 'Markdown 内容会实时预览；保存后参与关键词和向量检索。' : '富文本以 HTML 保存；切换回 Markdown 会转为纯文本，复杂格式可能丢失。' }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { Bold, Code2, Italic, Link2, List, ListOrdered, Underline } from 'lucide-vue-next'
import type { KnowledgeFormat } from '@/api/knowledge'

const props = withDefaults(defineProps<{ modelValue: string; format?: KnowledgeFormat; placeholder?: string }>(), {
  format: 'markdown',
  placeholder: '开始编写知识内容…',
})
const emit = defineEmits<{
  (event: 'update:modelValue', value: string): void
  (event: 'update:format', value: KnowledgeFormat): void
}>()

const localContent = ref(props.modelValue || '')
const localFormat = ref<KnowledgeFormat>(props.format || 'markdown')
const textarea = ref<HTMLTextAreaElement>()
const rich = ref<HTMLDivElement>()

const renderedMarkdown = computed(() => renderMarkdown(localContent.value))

watch(
  () => props.modelValue,
  (value) => {
    if (value === localContent.value) return
    localContent.value = value || ''
    if (localFormat.value === 'html') {
      nextTick(() => {
        if (rich.value && document.activeElement !== rich.value) rich.value.innerHTML = sanitizeHTML(localContent.value)
      })
    }
  },
)

watch(
  () => props.format,
  (value) => {
    if (!value || value === localFormat.value) return
    localFormat.value = value
    nextTick(syncRichContent)
  },
)

onMounted(syncRichContent)

function emitContent() {
  emit('update:modelValue', localContent.value)
}

function syncRichContent() {
  if (localFormat.value === 'html' && rich.value) {
    rich.value.innerHTML = sanitizeHTML(localContent.value)
  }
}

function onRichInput() {
  if (!rich.value) return
  localContent.value = rich.value.innerHTML
  emitContent()
}

function onPaste(event: ClipboardEvent) {
  const text = event.clipboardData?.getData('text/plain')
  if (text == null) return
  event.preventDefault()
  document.execCommand('insertText', false, text)
}

function switchFormat(format: KnowledgeFormat) {
  if (format === localFormat.value) return
  if (format === 'html') {
    localContent.value = renderMarkdown(localContent.value)
  } else {
    localContent.value = htmlToText(localContent.value)
  }
  localFormat.value = format
  emit('update:format', format)
  emitContent()
  nextTick(syncRichContent)
}

function wrapMarkdown(prefix: string, suffix: string) {
  const element = textarea.value
  if (!element) return
  const start = element.selectionStart
  const end = element.selectionEnd
  const selected = localContent.value.slice(start, end)
  localContent.value = localContent.value.slice(0, start) + prefix + selected + suffix + localContent.value.slice(end)
  emitContent()
  nextTick(() => {
    element.focus()
    element.selectionStart = start + prefix.length
    element.selectionEnd = end + prefix.length
  })
}

function insertLinePrefix(prefix: string) {
  const element = textarea.value
  if (!element) return
  const start = element.selectionStart
  const lineStart = localContent.value.lastIndexOf('\n', start - 1) + 1
  localContent.value = localContent.value.slice(0, lineStart) + prefix + localContent.value.slice(lineStart)
  emitContent()
  nextTick(() => {
    element.focus()
    element.selectionStart = element.selectionEnd = start + prefix.length
  })
}

function insertMarkdownLink() {
  const url = window.prompt('请输入链接地址', 'https://')
  if (!url) return
  wrapMarkdown('[', `](${url})`)
}

function exec(command: string, value?: string) {
  rich.value?.focus()
  document.execCommand(command, false, value)
  onRichInput()
}

function execBlock(tag: string) {
  exec('formatBlock', tag)
}

function insertRichLink() {
  const url = window.prompt('请输入链接地址', 'https://')
  if (url) exec('createLink', url)
}

function insertRichCode() {
  const selection = window.getSelection()?.toString() || ''
  const escaped = selection.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
  exec('insertHTML', `<code>${escaped || '代码'}</code>`)
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
  html = html.replace(/^> (.*)$/gm, '<blockquote>$1</blockquote>')
  html = html.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')
  html = html.replace(/`([^`]+)`/g, '<code>$1</code>')
  html = html.replace(/^- (.*)$/gm, '<li>$1</li>')
  html = html.replace(/\n/g, '<br>')
  return html
}

function htmlToText(value: string) {
  const container = document.createElement('div')
  container.innerHTML = sanitizeHTML(value)
  return (container.textContent || '').replace(/\n{3,}/g, '\n\n').trim()
}

function sanitizeHTML(value: string) {
  const allowed = new Set(['H1', 'H2', 'H3', 'P', 'BR', 'STRONG', 'B', 'EM', 'I', 'U', 'UL', 'OL', 'LI', 'CODE', 'PRE', 'BLOCKQUOTE', 'A', 'SPAN', 'DIV'])
  const container = document.createElement('div')
  container.innerHTML = value || ''
  const walk = (node: Node) => {
    for (const child of Array.from(node.childNodes)) {
      if (child.nodeType === Node.COMMENT_NODE) {
        child.remove()
        continue
      }
      if (child.nodeType === Node.ELEMENT_NODE) {
        const element = child as HTMLElement
        if (!allowed.has(element.tagName)) {
          element.replaceWith(document.createTextNode(element.textContent || ''))
          continue
        }
        for (const attribute of Array.from(element.attributes)) {
          if (element.tagName === 'A' && attribute.name === 'href') {
            if (!/^https?:\/\//i.test(attribute.value) && !attribute.value.startsWith('/')) element.removeAttribute('href')
            continue
          }
          element.removeAttribute(attribute.name)
        }
        walk(element)
      }
    }
  }
  walk(container)
  return container.innerHTML
}
</script>

<style scoped>
.knowledge-rich-editor { display: grid; gap: 8px; }
.editor-toolbar { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; padding: 8px; border: 1px solid #21445d; background: #071b2c; }
.mode-switch { display: inline-flex; border: 1px solid #21445d; }
.mode-switch button { border: 0; border-right: 1px solid #21445d; padding: 6px 10px; background: transparent; color: #8ba9bc; cursor: pointer; }
.mode-switch button:last-child { border-right: 0; }
.mode-switch button.active { background: #087c9e; color: #fff; }
.editor-tools { display: flex; gap: 4px; flex-wrap: wrap; }
.editor-tools button { display: inline-flex; align-items: center; justify-content: center; min-width: 28px; height: 28px; padding: 0 6px; border: 1px solid #21445d; background: #0b263b; color: #bdd8e9; cursor: pointer; }
.editor-tools button:hover { border-color: #22d3ee; }
.editor-tools svg { width: 13px; height: 13px; }
.editor-surface { display: grid; border: 1px solid #21445d; background: #061725; min-height: 300px; }
.editor-surface.split { grid-template-columns: 1fr 1fr; }
.markdown-input, .rich-input { width: 100%; min-height: 300px; padding: 14px; border: 0; outline: 0; background: #061725; color: #dcecff; font: 13px/1.7 ui-monospace, SFMono-Regular, Menlo, monospace; resize: vertical; }
.rich-input { font-family: inherit; }
.rich-input:empty::before { content: attr(data-placeholder); color: #526b7e; }
.markdown-preview { padding: 14px; border-left: 1px solid #21445d; overflow: auto; color: #b8cedb; }
.markdown-preview :deep(h1), .markdown-preview :deep(h2), .markdown-preview :deep(h3) { color: #e8f6ff; margin: 12px 0 6px; }
.markdown-preview :deep(code) { padding: 2px 5px; background: #0c2b3d; color: #7dd3fc; }
.markdown-preview :deep(pre) { padding: 12px; overflow: auto; background: #020b12; border: 1px solid #173b55; }
.markdown-preview :deep(blockquote) { margin: 8px 0; padding-left: 10px; border-left: 3px solid #1d7a9a; color: #8ba9bc; }
.markdown-preview :deep(li) { margin-left: 18px; }
.editor-hint { margin: 0; color: #617e92; font-size: 10px; }
@media (max-width: 900px) { .editor-surface.split { grid-template-columns: 1fr; } .markdown-preview { border-left: 0; border-top: 1px solid #21445d; } }
</style>
