<script setup lang="ts">
import { computed } from 'vue'
import type { LibraryIssue } from '../api/types'
import { count } from '../lib/format'
import { issueNote } from '../lib/notes'
import { isGuide, issueDetail, issueName } from '../lib/tags'
import { usePhone } from '../lib/viewport'
import { useEditor } from '../stores/editor'
import { useHelp } from '../stores/help'
import LibTip from './LibTip.vue'

const emit = defineEmits<{ cover: [] }>()
const phone = usePhone()
const editor = useEditor()
const help = useHelp()
const issues = computed(() => (editor.album?.issues ?? []).filter((i) => !isGuide(i.kind)))
const coverIssue = (i: LibraryIssue) => i.kind === 'no_cover' || i.kind === 'bad_cover'
</script>

<template>
  <template v-if="issues.length > 0">
    <template v-if="!phone">
      <div class="lib-h"><span>{{ count(issues.length, 'issue') }}</span></div>
      <div v-for="i in issues" :key="i.kind" class="lib-iss">
        <span class="led iss" role="img" aria-label="Open issue"></span>
        <span class="two"><span class="ttl"><LibTip :title="issueName(i.kind)" :note="issueNote(i.kind)">{{ issueName(i.kind) }}</LibTip></span><span class="sub">{{ issueDetail(i) }}</span></span>
        <button v-if="coverIssue(i)" type="button" class="key sm" @click="emit('cover')">{{ i.kind === 'bad_cover' ? 'Repair' : 'Cover' }}</button>
        <button v-else-if="editor.fixable(i)" type="button" class="key sm" @click="editor.applyFix(i)">Fix</button>
      </div>
    </template>
    <template v-else>
      <section v-for="i in issues" :key="i.kind" class="smod">
        <div class="smod-h"><span class="led iss"></span><h2 class="iss-name">{{ issueName(i.kind) }}</h2><button v-if="issueNote(i.kind)" type="button" class="wbtn q" :aria-label="`Why this matters: ${issueName(i.kind)}`" :aria-pressed="help.note?.title === issueName(i.kind)" @click="help.note = { title: issueName(i.kind), text: issueNote(i.kind) }">?</button></div>
        <p class="t2">{{ issueDetail(i) }}</p>
        <div class="smod-keys">
          <button v-if="coverIssue(i)" type="button" class="key" @click="emit('cover')">{{ i.kind === 'bad_cover' ? 'Repair' : 'Cover' }}</button>
          <button v-else-if="editor.fixable(i)" type="button" class="key" @click="editor.applyFix(i)">Fix</button>
        </div>
      </section>
    </template>
  </template>
</template>

<style scoped>
.rs .iss-name { white-space: normal; font-size: 15px; letter-spacing: .06em; line-height: 1.2; }
</style>
