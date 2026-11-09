<script setup lang="ts">
defineProps<{ label: string }>()
</script>

<template>
  <div class="mobile-card-row">
    <span class="mobile-label">{{ label }}</span>
    <span class="mobile-value"><slot /></span>
  </div>
</template>
