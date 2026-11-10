<script setup lang="ts">
import { useId } from 'vue'

// A connection's red patch cable in its jack. The parent's data-jack moves the plug.
const nut = `jk-${useId()}`
</script>

<template>
  <svg class="jack" width="78" height="44" viewBox="0 0 78 44" aria-hidden="true">
    <defs><radialGradient :id="nut" cx="38%" cy="30%" r="75%"><stop offset="0" stop-color="#F4F6F8" /><stop offset=".55" stop-color="#A3AAB4" /><stop offset="1" stop-color="#3F454E" /></radialGradient></defs>
    <circle cx="60" cy="22" r="15" :fill="`url(#${nut})`" stroke="#0A0C10" stroke-width="1.5" />
    <circle cx="60" cy="22" r="7" fill="#07090C" />
    <g class="plug">
      <path d="M14 22 C 4 22, 2 34, -8 44" fill="none" stroke="#0A0C10" stroke-width="7" stroke-linecap="round" opacity=".45" />
      <path d="M14 22 C 4 22, 2 34, -8 44" fill="none" stroke="#D9483B" stroke-width="5" stroke-linecap="round" />
      <rect x="14" y="15" width="22" height="14" rx="3" fill="#2A2F37" stroke="#0A0C10" />
      <path d="M19 16v12M23 16v12M27 16v12" stroke="rgba(255,255,255,.18)" stroke-width="1.5" />
      <rect x="36" y="18.5" width="16" height="7" rx="1" fill="#C9CED6" stroke="#0A0C10" />
      <rect x="52" y="19.5" width="8" height="5" rx="2.5" fill="#E2B84B" stroke="#0A0C10" />
    </g>
  </svg>
</template>
