<script setup lang="ts">
import { keyGroups } from '../lib/libraryKeys'
import { useHelp } from '../stores/help'
import Icon from './Icon.vue'
import LibGuide from './LibGuide.vue'

const help = useHelp()
const left = keyGroups.slice(0, 2)
const middle = keyGroups.slice(2, 3)
const right = keyGroups.slice(3)
</script>

<template>
  <div v-if="help.sheet" class="scrim" @keydown.esc="help.sheet = null">
    <section class="win dlg" role="dialog" aria-modal="true" aria-labelledby="dk-t">
      <div class="tb"><h2 id="dk-t">Help</h2><span class="stripes"></span><button type="button" class="wbtn" aria-label="Close" @click="help.sheet = null"><Icon name="close" /></button></div>
      <div class="seg"><div class="segk" role="radiogroup" aria-label="Help section"><button type="button" role="radio" :aria-checked="help.sheet === 'guide'" @click="help.sheet = 'guide'">Tagging guide</button><button type="button" role="radio" :aria-checked="help.sheet === 'keys'" @click="help.sheet = 'keys'">Keyboard</button></div></div>
      <div class="body">
        <LibGuide v-if="help.sheet === 'guide'" />
        <div v-else class="keys">
          <div v-for="(col, n) in [left, middle, right]" :key="n" class="col">
            <template v-for="g in col" :key="g.name">
              <h3 class="px t3">{{ g.name }}</h3>
              <div v-for="k in g.keys" :key="k.what" class="kc-row"><span>{{ k.what }}</span><span class="kc-keys"><kbd v-for="c in k.keys" :key="c" class="kc">{{ c }}</kbd></span></div>
            </template>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.rs .scrim { position: fixed; inset: 0; z-index: 40; display: flex; align-items: center; justify-content: center; padding: 20px; background: rgba(6, 8, 12, .6); }
.rs .dlg { width: 100%; max-width: 960px; }
.rs .seg { padding: 12px 18px 0; }
.rs .body { max-height: calc(100vh - 190px); overflow-y: auto; scrollbar-width: thin; }
.rs .keys { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(270px, 100%), 1fr)); gap: 8px 28px; padding: 6px 18px 20px; }
.rs h3 { padding: 10px 0 6px; font-size: 12px; }
</style>
