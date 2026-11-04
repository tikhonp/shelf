<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { get } from '../api/client'
import { keyTarget, tagKey, tagRows, type TagRow } from '../lib/tags'
import { useEditor } from '../stores/editor'
import Icon from './Icon.vue'
import LibTagRow from './LibTagRow.vue'

defineEmits<{ long: [TagRow] }>()
const editor = useEditor()
const known = ref<string[]>([])
const adding = ref(false)
const newKey = ref('')
const newValue = ref('')

onMounted(async () => {
  known.value = (await get<{ keys: string[] }>('/api/library/keys').catch(() => ({ keys: [] }))).keys
})

const rows = computed(() => tagRows(editor.scope, editor.stage, known.value))
const standard = computed(() => rows.value.filter((r) => r.group === 'standard'))
const extended = computed(() => rows.value.filter((r) => r.group === 'extended'))

// A key that already has a row goes to that row, so two rows never edit one tag.
function add() {
  const key = tagKey(newKey.value)
  if (!key) return
  const row = rows.value.find((r) => r.key === key)
  editor.edit(row?.target ?? keyTarget(key), newValue.value === '' ? [] : [newValue.value])
  newKey.value = ''
  newValue.value = ''
  adding.value = false
}
</script>

<template>
  <div class="lib-fields">
    <div class="lib-h"><span>Standard</span><span class="low">Navidrome reads these</span></div>
    <LibTagRow v-for="r in standard" :key="r.key" :row="r" @long="$emit('long', r)" />
    <div class="lib-h"><span>Extended</span><span class="low">Navidrome ignores these</span></div>
    <LibTagRow v-for="r in extended" :key="r.key" :row="r" @long="$emit('long', r)" />
    <form v-if="adding" class="lf tagrow" @submit.prevent="add">
      <input v-model="newKey" class="lib-in" placeholder="KEY" aria-label="New field's key" />
      <div class="lf-c">
        <input v-model="newValue" class="lib-in" placeholder="Value" aria-label="New field's value" />
        <span class="lf-adds"><button type="submit" class="lf-add" :disabled="!tagKey(newKey)"><Icon name="check" size="s" />Add</button><button type="button" class="lf-add" @click="adding = false">Cancel</button></span>
      </div>
    </form>
    <div v-else class="add"><button type="button" class="lf-add" @click="adding = true"><Icon name="plus" size="s" />Add field</button></div>
  </div>
</template>

<style scoped>
.rs .add { padding: 8px 14px 12px; }
</style>
