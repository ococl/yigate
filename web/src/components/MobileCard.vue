<script setup lang="ts">
withDefaults(defineProps<{ highlight?: boolean }>(), { highlight: false })
</script>

<template>
  <a-card size="small" class="mobile-card" :class="{ 'row-highlight': highlight }">
    <div v-if="$slots.head || $slots.extra" class="mobile-card-head">
      <slot name="head" />
      <slot name="extra" />
    </div>
    <slot />
    <div v-if="$slots.actions" class="mobile-card-actions">
      <slot name="actions" />
    </div>
  </a-card>
</template>
