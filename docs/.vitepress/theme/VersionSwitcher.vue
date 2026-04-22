<script setup lang="ts">
import { ref, onMounted } from 'vue'

interface VersionEntry {
  version: string
  path: string
  latest?: boolean
}

const versions = ref<VersionEntry[]>([])
const currentVersion = ref('dev')
const isOpen = ref(false)
const loaded = ref(false)

function detectCurrentVersion(): string {
  if (typeof window === 'undefined') return 'dev'
  // Path format: /aether/dev/ or /aether/v0.4.0/...
  const match = window.location.pathname.match(/^\/aether\/(v[\d.]+|dev)/)
  return match ? match[1] : 'dev'
}

function navigate(entry: VersionEntry) {
  isOpen.value = false
  if (entry.version === currentVersion.value) return
  window.location.href = entry.path
}

function closeDropdown(e: MouseEvent) {
  const target = e.target as HTMLElement
  if (!target.closest('.version-switcher')) {
    isOpen.value = false
  }
}

onMounted(async () => {
  currentVersion.value = detectCurrentVersion()

  try {
    const res = await fetch('/aether/versions.json')
    if (res.ok) {
      versions.value = await res.json()
      loaded.value = true
    }
  } catch {
    // Graceful fallback: just show the current version text
  }

  document.addEventListener('click', closeDropdown)
})
</script>

<template>
  <div class="version-switcher" v-if="loaded && versions.length > 0">
    <button class="version-button" @click="isOpen = !isOpen" :aria-expanded="isOpen">
      {{ currentVersion }}
      <svg class="caret" xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24"
        fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <polyline points="6 9 12 15 18 9" />
      </svg>
    </button>
    <div class="version-menu" v-show="isOpen">
      <button
        v-for="entry in versions"
        :key="entry.version"
        class="version-item"
        :class="{ active: entry.version === currentVersion }"
        @click="navigate(entry)"
      >
        {{ entry.version }}
        <span v-if="entry.latest" class="latest-badge">latest</span>
      </button>
    </div>
  </div>
  <span v-else-if="!loaded" class="version-text">{{ currentVersion }}</span>
</template>

<style scoped>
.version-switcher {
  position: relative;
  margin-right: 16px;
  padding-right: 16px;
  border-right: 1px solid var(--vp-c-divider);
}

.version-button {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 0 8px;
  height: 36px;
  border: 1px solid var(--vp-c-divider);
  border-radius: 8px;
  background: transparent;
  color: var(--vp-c-text-1);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition: border-color 0.25s, color 0.25s;
}

.version-button:hover {
  border-color: var(--vp-c-brand-1);
  color: var(--vp-c-brand-1);
}

.caret {
  transition: transform 0.25s;
}

.version-button[aria-expanded="true"] .caret {
  transform: rotate(180deg);
}

.version-menu {
  position: absolute;
  top: calc(100% + 4px);
  left: 0;
  min-width: 120px;
  padding: 4px;
  border: 1px solid var(--vp-c-divider);
  border-radius: 8px;
  background: var(--vp-c-bg-elv);
  box-shadow: var(--vp-shadow-3);
  z-index: 100;
}

.version-item {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
  padding: 6px 10px;
  border: none;
  border-radius: 6px;
  background: transparent;
  color: var(--vp-c-text-2);
  font-size: 13px;
  cursor: pointer;
  transition: background-color 0.25s, color 0.25s;
}

.version-item:hover {
  background: var(--vp-c-default-soft);
  color: var(--vp-c-text-1);
}

.version-item.active {
  color: var(--vp-c-brand-1);
  font-weight: 600;
}

.latest-badge {
  font-size: 10px;
  padding: 1px 5px;
  border-radius: 4px;
  background: var(--vp-c-brand-soft);
  color: var(--vp-c-brand-1);
  font-weight: 600;
}

.version-text {
  margin-right: 16px;
  padding-right: 16px;
  border-right: 1px solid var(--vp-c-divider);
  font-size: 13px;
  color: var(--vp-c-text-2);
}
</style>
