<script setup lang="ts">
import { monitorApi } from "@/api/monitor";
import GroupFormModal from "@/components/keys/GroupFormModal.vue";
import GroupMonitorCard from "@/components/monitor/GroupMonitorCard.vue";
import type { Group, GroupUsageData } from "@/types/models";
import { ReloadOutline } from "@vicons/ionicons5";
import { NButton, NEmpty, NIcon, NSpin, useMessage } from "naive-ui";
import { computed, onMounted, onUnmounted, ref } from "vue";
import { useI18n } from "vue-i18n";

const { t } = useI18n();
const message = useMessage();

interface GroupWithUsage extends Group {
  usage_data?: GroupUsageData;
}

type FilterType = "all" | "normal" | "expired" | "quota-exceeded";

const loading = ref(false);
const rawGroups = ref<GroupWithUsage[]>([]); // 原始数据（未排序）
const groupSortOrder = ref<number[]>([]); // 分组ID排序
const selectedGroup = ref<Group | null>(null);
const showEditModal = ref(false);
const currentFilter = ref<FilterType>("all");

// 拖拽相关状态
const draggedItem = ref<GroupWithUsage | null>(null);
const draggedOverIndex = ref<number>(-1);

// 自动刷新定时器
let refreshTimer: ReturnType<typeof setInterval> | null = null;
const AUTO_REFRESH_INTERVAL = 30000; // 30秒

// 加载排序配置
async function loadSortOrder() {
  try {
    const order = await monitorApi.getGroupSortOrder();
    groupSortOrder.value = order;
  } catch (error) {
    // 如果加载失败（文件不存在），使用空数组
    groupSortOrder.value = [];
  }
}

// 保存排序配置
async function saveSortOrder(order: number[]) {
  try {
    await monitorApi.saveGroupSortOrder(order);
    groupSortOrder.value = order;
  } catch (error) {
    console.error("Failed to save sort order:", error);
    message.error(t("groupMonitor.saveSortFailed"));
  }
}

// 根据排序规则对分组进行排序
const groups = computed(() => {
  if (groupSortOrder.value.length === 0) {
    return rawGroups.value;
  }

  // 创建分组ID到分组的映射（只包含有id的分组）
  const groupMap = new Map<number, GroupWithUsage>();
  rawGroups.value.forEach(g => {
    if (g.id !== undefined) {
      groupMap.set(g.id, g);
    }
  });

  // 按照排序顺序获取分组
  const sorted: GroupWithUsage[] = [];
  for (const id of groupSortOrder.value) {
    const group = groupMap.get(id);
    if (group) {
      sorted.push(group);
      groupMap.delete(id);
    }
  }

  // 添加新分组（不在排序中的）
  for (const group of groupMap.values()) {
    sorted.push(group);
  }

  return sorted;
});

// 获取监控数据
async function loadMonitorData() {
  try {
    loading.value = true;
    const response = await monitorApi.getGroupMonitorData();
    rawGroups.value = response.groups || [];
  } catch (error) {
    console.error("Failed to load monitor data:", error);
    message.error(t("groupMonitor.loadingFailed"));
  } finally {
    loading.value = false;
  }
}

// 刷新数据
function refreshData() {
  loadMonitorData();
}

// 编辑分组
function handleEditGroup(group: Group) {
  selectedGroup.value = group;
  showEditModal.value = true;
}

// 分组更新完成
function handleGroupUpdated() {
  showEditModal.value = false;
  selectedGroup.value = null;
  // 刷新数据以更新显示
  loadMonitorData();
}

// 关闭编辑弹窗
function handleEditModalClose() {
  showEditModal.value = false;
  selectedGroup.value = null;
}

// 组件挂载时加载数据并启动自动刷新
onMounted(async () => {
  await loadSortOrder();
  await loadMonitorData();
  // 启动自动刷新
  refreshTimer = setInterval(() => {
    loadMonitorData();
  }, AUTO_REFRESH_INTERVAL);
});

// 组件卸载时清除定时器
onUnmounted(() => {
  if (refreshTimer) {
    clearInterval(refreshTimer);
    refreshTimer = null;
  }
});

// 统计信息
const stats = computed(() => {
  const total = groups.value.length;
  const expired = groups.value.filter(g => {
    const expiresAt = g.config?.expires_at as string | undefined;
    return expiresAt && new Date(expiresAt) < new Date();
  }).length;
  const quotaExceeded = groups.value.filter(g => {
    if (!g.usage_data) return false;
    const { hourly_usage, monthly_usage, hourly_limit, monthly_limit } = g.usage_data;
    return (
      (hourly_limit > 0 && hourly_usage >= hourly_limit) ||
      (monthly_limit > 0 && monthly_usage >= monthly_limit)
    );
  }).length;
  const normal = total - expired - quotaExceeded;

  return { total, expired, quotaExceeded, normal };
});

// 获取分组状态
function getGroupStatus(group: GroupWithUsage): FilterType {
  const expiresAt = group.config?.expires_at as string | undefined;
  if (expiresAt && new Date(expiresAt) < new Date()) {
    return "expired";
  }

  if (group.usage_data) {
    const { hourly_usage, monthly_usage, hourly_limit, monthly_limit } = group.usage_data;
    if (
      (hourly_limit > 0 && hourly_usage >= hourly_limit) ||
      (monthly_limit > 0 && monthly_usage >= monthly_limit)
    ) {
      return "quota-exceeded";
    }
  }

  return "normal";
}

// 根据筛选条件过滤分组
const filteredGroups = computed(() => {
  if (currentFilter.value === "all") {
    return groups.value;
  }
  return groups.value.filter(g => getGroupStatus(g) === currentFilter.value);
});

// 设置筛选条件
function setFilter(filter: FilterType) {
  currentFilter.value = filter;
}

// 拖拽开始
function handleDragStart(event: DragEvent, group: GroupWithUsage) {
  draggedItem.value = group;
  if (event.dataTransfer) {
    event.dataTransfer.effectAllowed = "move";
    event.dataTransfer.setData("text/plain", String(group.id));
  }
}

// 拖拽经过
function handleDragOver(event: DragEvent, index: number) {
  event.preventDefault();
  if (event.dataTransfer) {
    event.dataTransfer.dropEffect = "move";
  }
  draggedOverIndex.value = index;
}

// 拖拽离开
function handleDragLeave() {
  draggedOverIndex.value = -1;
}

// 放置（拖拽结束）
function handleDrop(event: DragEvent, targetGroup: GroupWithUsage) {
  event.preventDefault();
  draggedOverIndex.value = -1;

  if (!draggedItem.value || !draggedItem.value.id || !targetGroup.id || draggedItem.value.id === targetGroup.id) {
    draggedItem.value = null;
    return;
  }

  // 创建新的排序数组（只包含有id的分组）
  const currentOrder = groups.value
    .map(g => g.id)
    .filter((id): id is number => id !== undefined);
  const newOrder = [...currentOrder];

  // 找到被拖拽项和目标项的索引
  const draggedIndex = newOrder.indexOf(draggedItem.value.id);
  const targetIndex = newOrder.indexOf(targetGroup.id);

  if (draggedIndex !== -1 && targetIndex !== -1) {
    // 移除被拖拽项
    newOrder.splice(draggedIndex, 1);
    // 插入到目标位置
    newOrder.splice(targetIndex, 0, draggedItem.value.id);

    // 保存新排序
    saveSortOrder(newOrder);
  }

  draggedItem.value = null;
}

// 拖拽结束
function handleDragEnd() {
  draggedItem.value = null;
  draggedOverIndex.value = -1;
}
</script>

<template>
  <div class="group-monitor-container">
    <!-- 顶部操作栏 -->
    <div class="monitor-header">
      <div class="header-left">
        <h2 class="page-title">{{ t("groupMonitor.title") }}</h2>
        <div class="stats-summary">
          <span
            class="stat-item"
            :class="{ active: currentFilter === 'all' }"
            @click="setFilter('all')"
          >
            {{ t("groupMonitor.filterAll") }}: {{ stats.total }}
          </span>
          <span
            class="stat-item"
            :class="{ active: currentFilter === 'normal' }"
            @click="setFilter('normal')"
          >
            {{ t("groupMonitor.statusNormal") }}: {{ stats.normal }}
          </span>
          <span
            class="stat-item stat-warning"
            :class="{ active: currentFilter === 'quota-exceeded' }"
            @click="setFilter('quota-exceeded')"
          >
            {{ t("groupMonitor.statusQuotaExceeded") }}: {{ stats.quotaExceeded }}
          </span>
          <span
            class="stat-item stat-error"
            :class="{ active: currentFilter === 'expired' }"
            @click="setFilter('expired')"
          >
            {{ t("groupMonitor.statusExpired") }}: {{ stats.expired }}
          </span>
        </div>
      </div>
      <div class="header-actions">
        <n-button @click="refreshData" :loading="loading" secondary>
          <template #icon>
            <n-icon :component="ReloadOutline" />
          </template>
          {{ t("groupMonitor.refreshData") }}
        </n-button>
      </div>
    </div>

    <!-- 卡片网格 -->
    <n-spin :show="loading">
      <div v-if="filteredGroups.length === 0 && !loading" class="empty-container">
        <n-empty :description="t('groupMonitor.noGroups')" />
      </div>
      <div v-else class="cards-container">
        <div
          v-for="(group, index) in filteredGroups"
          :key="group.id"
          class="card-wrapper"
          :class="{
            'dragging': draggedItem?.id === group.id,
            'drag-over': draggedOverIndex === index
          }"
          draggable="true"
          @dragstart="handleDragStart($event, group)"
          @dragover="handleDragOver($event, index)"
          @dragleave="handleDragLeave"
          @drop="handleDrop($event, group)"
          @dragend="handleDragEnd"
        >
          <group-monitor-card
            :group="group"
            :usage-data="group.usage_data"
            @edit="handleEditGroup"
          />
        </div>
      </div>
    </n-spin>

    <!-- 编辑弹窗 -->
    <group-form-modal
      v-model:show="showEditModal"
      :group="selectedGroup"
      @success="handleGroupUpdated"
      @update:show="handleEditModalClose"
    />
  </div>
</template>

<style scoped>
.group-monitor-container {
  padding: 20px;
  min-height: calc(100vh - 120px);
  background: var(--bg-primary);
  border-radius: 16px;
}

.monitor-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 24px;
  flex-wrap: wrap;
  gap: 16px;
}

.header-left {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.page-title {
  font-size: 24px;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0;
}

.stats-summary {
  display: flex;
  gap: 16px;
  flex-wrap: wrap;
}

.stat-item {
  font-size: 13px;
  color: var(--text-secondary);
  padding: 4px 12px;
  background: var(--bg-secondary);
  border-radius: 12px;
  cursor: pointer;
  transition: all 0.2s ease;
  user-select: none;
}

.stat-item:hover {
  background: var(--bg-tertiary);
}

.stat-item.active {
  font-weight: 600;
  box-shadow: 0 0 0 2px var(--primary-color);
}

.stat-warning {
  color: #f0a020;
  background: rgba(240, 160, 32, 0.1);
}

.stat-error {
  color: #d03050;
  background: rgba(208, 48, 80, 0.1);
}

.stat-total {
  color: var(--primary-color);
  background: rgba(102, 126, 234, 0.1);
  font-weight: 600;
}

.header-actions {
  display: flex;
  gap: 12px;
}

.empty-container {
  display: flex;
  justify-content: center;
  align-items: center;
  min-height: 300px;
}

/* 卡片容器 - 使用 CSS Grid 实现响应式布局 */
.cards-container {
  display: grid;
  grid-template-columns: repeat(1, 1fr);
  gap: 16px;
}

@media (min-width: 640px) {
  .cards-container {
    grid-template-columns: repeat(2, 1fr);
  }
}

@media (min-width: 1024px) {
  .cards-container {
    grid-template-columns: repeat(3, 1fr);
  }
}

@media (min-width: 1280px) {
  .cards-container {
    grid-template-columns: repeat(5, 1fr);
  }
}

/* 拖拽相关样式 */
.card-wrapper {
  cursor: grab;
  transition: transform 0.2s ease, box-shadow 0.2s ease, border-color 0.2s ease;
  border-radius: 8px;
  padding: 2px;
}

.card-wrapper:active {
  cursor: grabbing;
}

.card-wrapper.dragging {
  opacity: 0.5;
  transform: scale(0.95);
}

.card-wrapper.drag-over {
  border: 2px dashed var(--primary-color);
  background: rgba(102, 126, 234, 0.05);
  border-radius: 8px;
  padding: 0;
}

/* 响应式适配 */
@media (max-width: 768px) {
  .group-monitor-container {
    padding: 16px;
  }

  .monitor-header {
    flex-direction: column;
    align-items: flex-start;
  }

  .page-title {
    font-size: 20px;
  }

  .stats-summary {
    gap: 8px;
  }

  .stat-item {
    font-size: 12px;
    padding: 3px 10px;
  }
}
</style>
