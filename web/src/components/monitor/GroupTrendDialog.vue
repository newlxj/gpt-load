<script setup lang="ts">
import LineChart from "@/components/LineChart.vue";
import type { Group } from "@/types/models";
import { getGroupDisplayName } from "@/utils/display";
import { CloseOutline } from "@vicons/ionicons5";
import { NButton, NCard, NIcon, NModal } from "naive-ui";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

interface Props {
  show: boolean;
  group: Group | null;
}

interface Emits {
  (e: "update:show", value: boolean): void;
}

const props = defineProps<Props>();
const emit = defineEmits<Emits>();
const { t } = useI18n();
const chartRenderKey = ref(0);

const modalVisible = computed({
  get: () => props.show,
  set: (value: boolean) => emit("update:show", value),
});

const chartTitle = computed(() => {
  if (!props.group) {
    return t("charts.requestTrend24h");
  }

  return `${getGroupDisplayName(props.group)} - ${t("charts.requestTrend24h")}`;
});

watch(
  () => props.show,
  show => {
    if (show) {
      chartRenderKey.value += 1;
    }
  }
);

function handleModalUpdate(show: boolean) {
  modalVisible.value = show;
}
</script>

<template>
  <n-modal :show="modalVisible" @update:show="handleModalUpdate" class="group-trend-modal">
    <n-card
      class="group-trend-card"
      :title="chartTitle"
      :bordered="false"
      size="huge"
      role="dialog"
      aria-modal="true"
    >
      <template #header-extra>
        <n-button quaternary circle @click="modalVisible = false">
          <template #icon>
            <n-icon :component="CloseOutline" />
          </template>
        </n-button>
      </template>

      <line-chart
        v-if="group?.id"
        :key="`${group.id}-${chartRenderKey}`"
        :fixed-group-id="group.id"
        :show-group-selector="false"
        :show-range-selector="true"
        :default-hours="24"
        :title="chartTitle"
      />
    </n-card>
  </n-modal>
</template>

<style scoped>
.group-trend-modal {
  width: min(980px, 96vw);
  --n-color: var(--modal-color);
}

.group-trend-card {
  max-height: 90vh;
  overflow: auto;
}

:deep(.n-card-header) {
  border-bottom: 1px solid var(--border-color);
  padding: 12px 20px;
}

:deep(.n-card__content) {
  padding: 16px 20px 20px;
}
</style>
