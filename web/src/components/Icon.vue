<script lang="ts">
const circle = (x: number, y: number, r: number) => `M${x - r} ${y}a${r} ${r} 0 1 0 ${2 * r} 0a${r} ${r} 0 1 0 ${-2 * r} 0`

const shapes = {
  search: [circle(10.5, 10.5, 6.5), 'm20 20-4.5-4.5'],
  download: ['M12 4v11', 'm7 10 5 5 5-5', 'M5 20h14'],
  status: ['M4 17a8 8 0 1 1 16 0', 'M12 17l4-5'],
  clock: [circle(12, 12, 8), 'M12 8v4l3 2'],
  retry: ['M4 12a8 8 0 1 0 2.5-5.8', 'M4 4v5h5'],
  check: ['m5 12 4.5 4.5L19 7'],
  paste: ['M8 3h8v4H8z', 'M16 5h3v16H5V5h3'],
  close: ['M6 6l12 12M18 6 6 18'],
  stop: ['M6 6h12v12H6z'],
  chevron: ['m6 9 6 6 6-6'],
  next: ['m9 6 6 6-6 6'],
  back: ['m15 6-6 6 6 6'],
  open: ['M14 4h6v6', 'M20 4l-9 9', 'M18 14v6H4V6h6'],
  sun: ['M12 3v2M12 19v2M3 12h2M19 12h2M5.6 5.6l1.4 1.4M17 17l1.4 1.4M5.6 18.4 7 17M17 7l1.4-1.4', circle(12, 12, 4)],
  moon: ['M20 14.5A8.5 8.5 0 1 1 9.5 4a7 7 0 0 0 10.5 10.5z'],
  scan: ['M20 12a8 8 0 0 1-14 5.3', 'M4 12a8 8 0 0 1 14-5.3', 'M18 3v4h-4', 'M6 21v-4h4'],
  user: [circle(12, 8, 4), 'M4 21a8 8 0 0 1 16 0'],
  library: ['M5 4v16', 'M10 4v16', 'M15 5l4 15'],
  lock: ['M6 11h12v9H6z', 'M8 11V8a4 4 0 0 1 8 0v3'],
  play: ['M7 5v14l12-7z'],
  pause: ['M8 5v14', 'M16 5v14'],
  plus: ['M12 5v14', 'M5 12h14'],
  revert: ['M9 7 4 12l5 5', 'M4 12h11a5 5 0 0 1 0 10h-2'],
  note: [circle(12, 12, 8), 'M12 11v5', 'M12 8h.01'],
  upload: ['M12 16V5', 'm7 10 5-5 5 5', 'M5 20h14'],
} as const

export type IconName = keyof typeof shapes
</script>

<script setup lang="ts">
defineProps<{ name: IconName; size?: 's' | 'l' }>()
</script>

<template>
  <svg class="i" :class="size" viewBox="0 0 24 24" aria-hidden="true">
    <path v-for="d in shapes[name]" :key="d" :d="d" />
  </svg>
</template>
