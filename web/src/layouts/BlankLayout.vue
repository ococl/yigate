<script setup lang="ts">
</script>

<template>
  <div class="blank-wrapper">
    <RouterView />
  </div>
</template>

<style scoped>
.blank-wrapper {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: #f0f2f5;
}

:global(body.app-dark) .blank-wrapper {
  background: #000;
}
</style>

