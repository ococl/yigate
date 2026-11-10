<script setup lang="ts">
withDefaults(defineProps<{ size?: number }>(), { size: 24 })
</script>

<template>
  <svg
    :width="size"
    :height="size"
    viewBox="0 0 48 48"
    class="app-logo"
    role="img"
    aria-label="YiGate"
  >
    <rect width="48" height="48" rx="12" fill="#0958d9" />
    <path
      d="M13 12 24 23 35 12"
      fill="none"
      stroke="#ffffff"
      stroke-width="6.4"
      stroke-linecap="round"
      stroke-linejoin="round"
    />
    <path d="M24 23V37" fill="none" stroke="#ffffff" stroke-width="6.4" stroke-linecap="round" />
    <circle cx="13" cy="12" r="2.1" fill="#0958d9" />
    <circle cx="35" cy="12" r="2.1" fill="#0958d9" />
  </svg>
</template>

<style scoped>
.app-logo {
  flex: 0 0 auto;
  display: block;
}
</style>
