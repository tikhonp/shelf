<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { count } from '../lib/format'
import { libraryTape } from '../lib/quality'
import { scopeNote } from '../lib/notes'
import { fieldView, isGuide, mainFields, offersFor, type MainField } from '../lib/tags'
import { useEditor } from '../stores/editor'
import { useHelp } from '../stores/help'
import AlbumProposals from './AlbumProposals.vue'
import Cover from './Cover.vue'
import Icon from './Icon.vue'
import LibLoading from './LibLoading.vue'
import LibScope from './LibScope.vue'
import LibSource from './LibSource.vue'
import LibSug from './LibSug.vue'
import LibTracks from './LibTracks.vue'
import LibUnavailable from './LibUnavailable.vue'

defineEmits<{ cover: [] }>()
const editor = useEditor()
const help = useHelp()
const router = useRouter()

const issues = computed(() => (editor.album?.issues ?? []).filter((i) => !isGuide(i.kind)))
const url = computed(() => (editor.cover?.delete ? '' : (editor.cover?.preview ?? editor.coverUrl)))
const view = (f: MainField) => fieldView(editor.scope, f.field, editor.stage)
const shown = (f: MainField) => {
  const v = view(f)
  if (v.mixed) return 'Mixed'
  return v.values.join(', ') || 'Empty'
}
const none = (f: MainField) => view(f).mixed || view(f).values.length === 0
const locked = (f: MainField) => !!f.perTrack && !editor.selected
const best = (f: MainField) => offersFor(editor.suggestions, f.field, editor.trackId, locked(f), view(f).values)[0]
const openField = (f: MainField) => router.push({ query: { field: f.field } })
const flagOn = (f: MainField) => view(f).values[0] === '1'
</script>

<template>
  <section class="win" aria-labelledby="la-t">
    <div class="tb">
      <button type="button" class="wbtn" aria-label="Back to the list" @click="router.push({ name: 'library' })"><Icon name="back" /></button>
      <h1 id="la-t">Album</h1>
      <span class="stripes"></span>
      <button v-if="editor.album" type="button" class="wbtn txt" :disabled="!!editor.saving || !!editor.deleting" @click="editor.remove()">Delete</button>
      <button type="button" class="wbtn q" aria-label="Note on this page" :aria-pressed="!!help.note" @click="help.note = help.note ? null : { title: 'Album', text: scopeNote }">?</button>
    </div>
    <LibLoading v-if="editor.loading" />
    <p v-else-if="editor.error" class="bad-text pad" role="alert">{{ editor.error }}</p>
    <div v-else-if="editor.album" class="vscroll">
      <div class="head">
        <button type="button" class="lib-cover" aria-label="Open the cover" @click="$emit('cover')"><Cover :url="url" :size="88" /></button>
        <div class="two">
          <h2>{{ editor.album.title }}</h2>
          <span class="sub by">{{ editor.album.artist }}</span>
          <span class="q"><span class="stk" :class="libraryTape(editor.album.quality).cls">{{ libraryTape(editor.album.quality).text }}</span></span>
          <LibSource :source="editor.album.source" />
        </div>
      </div>
      <button v-if="issues.length > 0" type="button" class="lib-irow gap" @click="router.push({ query: { view: 'issues' } })"><span class="led iss"></span><span>{{ count(issues.length, 'issue') }}</span><Icon name="next" /></button>
      <AlbumProposals :album-id="editor.album.id" />
      <LibUnavailable />
      <LibScope />
      <div v-for="f in mainFields" :key="f.field" class="pf">
        <button v-if="f.flag" type="button" class="pf-flag rocker" role="switch" :aria-checked="flagOn(f)" :aria-label="f.label" @click="editor.edit(f.field, flagOn(f) ? [] : ['1'])">
          <span class="two"><span class="pf-l">{{ f.label }}</span><span class="pf-v">{{ flagOn(f) ? 'On' : 'Off' }}</span></span>
          <span class="rk" aria-hidden="true">ON<span class="rk-case"><span class="rk-paddle"></span></span>OFF</span>
        </button>
        <div v-else-if="locked(f)" class="pf-open still">
          <span class="two"><span class="pf-l">{{ f.label }}</span><span class="pf-v none">{{ shown(f) }}</span></span>
          <Icon name="lock" />
        </div>
        <button v-else type="button" class="pf-open" :aria-label="`Edit ${f.label}, now ${shown(f)}`" @click="openField(f)">
          <span class="two"><span class="pf-l"><span v-if="view(f).staged" class="led stg" role="img" aria-label="Staged"></span>{{ f.label }}</span><span class="pf-v" :class="{ none: none(f) }">{{ shown(f) }}</span></span>
          <Icon name="next" />
        </button>
        <div v-if="best(f)" class="sugs"><LibSug :s="best(f)!" @use="editor.take(best(f)!, !!f.multi)" /></div>
      </div>
      <div class="lib-h static"><span>Tracks</span></div>
      <LibTracks />
    </div>
  </section>
</template>

<style scoped>
.rs .pad { padding: 14px; }
.rs .head { display: flex; align-items: center; gap: 14px; padding: 14px 12px; }
.rs .head .two { gap: 3px; }
.rs h2 { font-size: 19px; font-weight: 700; line-height: 1.2; }
.rs .by { font-size: 14px; }
.rs .q { margin-top: 5px; }
.rs .gap { margin-bottom: 10px; }
.rs .still { cursor: default; }
.rs .pf-open .two, .rs .pf-flag .two { gap: 2px; }
.rs .static { position: static; margin-top: 10px; border-top: 1px solid var(--ink); }
</style>
