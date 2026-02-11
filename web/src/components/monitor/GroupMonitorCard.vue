<script setup lang="ts">
import type { Group, GroupUsageData } from "@/types/models";
import { copy as copyToClipboard } from "@/utils/clipboard";
import { getGroupDisplayName } from "@/utils/display";
import { KeyOutline, LinkOutline, TimeOutline } from "@vicons/ionicons5";
import { NCard, NIcon, NProgress, NTag, useMessage } from "naive-ui";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";

type CardStatus = "normal" | "expired" | "quota-exceeded";

interface Props {
  group: Group;
  usageData?: GroupUsageData;
}

interface Emits {
  (e: "edit", group: Group): void;
}

const props = defineProps<Props>();
const emit = defineEmits<Emits>();
const { t } = useI18n();
const message = useMessage();

const isHovered = ref(false);

// 渠道符号映射
const channelSymbol = computed(() => {
  const symbols: Record<string, string> = {
    openai: "O",
    gemini: "G",
    anthropic: "A",
  };
  return symbols[props.group.channel_type] || "?";
});

// 获取渠道标签类型
function getChannelTagType(channelType: string): "success" | "info" | "warning" | "default" {
  switch (channelType) {
    case "openai":
      return "success";
    case "gemini":
      return "info";
    case "anthropic":
      return "warning";
    default:
      return "default";
  }
}

// 卡片状态计算
const cardStatus = computed<CardStatus>(() => {
  const expiresAt = props.group.config?.expires_at as string | undefined;
  if (expiresAt && new Date(expiresAt) < new Date()) {
    return "expired";
  }

  if (props.usageData) {
    const { hourly_usage, monthly_usage, hourly_limit, monthly_limit } = props.usageData;
    if (hourly_limit > 0 && hourly_usage >= hourly_limit) return "quota-exceeded";
    if (monthly_limit > 0 && monthly_usage >= monthly_limit) return "quota-exceeded";
  }

  return "normal";
});

const cardClass = computed(() => {
  return `monitor-card status-${cardStatus.value}`;
});

// 每小时进度条配置
const hourlyProgress = computed(() => {
  if (!props.usageData || props.usageData.hourly_limit === 0) {
    return { percentage: 0, show: false };
  }
  const percentage = Math.min(
    (props.usageData.hourly_usage / props.usageData.hourly_limit) * 100,
    100
  );
  return { percentage, show: true };
});

// 每月进度条配置
const monthlyProgress = computed(() => {
  if (!props.usageData || props.usageData.monthly_limit === 0) {
    return { percentage: 0, show: false };
  }
  const percentage = Math.min(
    (props.usageData.monthly_usage / props.usageData.monthly_limit) * 100,
    100
  );
  return { percentage, show: true };
});

// 进度条颜色
function getProgressColor(percentage: number): string {
  if (percentage >= 90) return "#d03050";
  if (percentage >= 70) return "#f0a020";
  return "#18a058";
}

// 过期时间显示
const expiresAtDisplay = computed(() => {
  const expiresAt = props.group.config?.expires_at as string | undefined;
  if (!expiresAt) return null;

  const date = new Date(expiresAt);
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
});

// 格式化数字
function formatNumber(num: number): string {
  if (num >= 1000000) return (num / 1000000).toFixed(1) + "M";
  if (num >= 1000) return (num / 1000).toFixed(1) + "K";
  return num.toString();
}

// 复制代理密钥
async function copyProxyKeys(e: Event) {
  e.stopPropagation();
  if (!props.group.proxy_keys) {
    message.warning(t("keys.noKeysToCopy"));
    return;
  }
  const success = await copyToClipboard(props.group.proxy_keys);
  if (success) {
    message.success(t("keys.keysCopiedToClipboard"));
  } else {
    message.error(t("keys.copyFailedManual"));
  }
}

// 复制端点地址
async function copyEndpoint(e: Event) {
  e.stopPropagation();
  if (!props.group.endpoint) {
    message.error(t("keys.copyFailed"));
    return;
  }
  const success = await copyToClipboard(props.group.endpoint);
  if (success) {
    message.success(t("keys.urlCopied"));
  } else {
    message.error(t("keys.copyFailedManual"));
  }
}

function handleCardClick() {
  emit("edit", props.group);
}
</script>

<template>
  <n-card
    :class="cardClass"
    hoverable
    @click="handleCardClick"
    class="group-monitor-card"
    :bordered="false"
    @mouseenter="isHovered = true"
    @mouseleave="isHovered = false"
  >
    <template #header>
      <div class="card-header">
        <div class="header-left">
          <span class="group-name">{{ getGroupDisplayName(group) }}</span>
          <span
            v-if="expiresAtDisplay"
            class="expiry-compact"
            :class="{ expired: cardStatus === 'expired' }"
          >
            {{ expiresAtDisplay }}
          </span>
        </div>
        <div class="header-right">
          <n-tag :type="getChannelTagType(group.channel_type)" size="tiny" round>
            {{ channelSymbol }}
          </n-tag>
          <transition name="fade">
            <div v-if="isHovered" class="action-buttons">
              <button class="action-btn" @click="copyProxyKeys" :title="t('keys.copyKeys')">
                <n-icon :size="14">
                  <KeyOutline />
                </n-icon>
              </button>
              <button class="action-btn" @click="copyEndpoint" :title="t('keys.copyUrl')">
                <n-icon :size="14">
                  <LinkOutline />
                </n-icon>
              </button>
            </div>
          </transition>
        </div>
      </div>
      <div class="card-content">
        <!-- 请求量统计 -->
        <div class="stats-row">
          <div class="stat-item" :title="t('groupMonitor.24hours')">
            <n-icon :size="10" class="stat-icon">
              <TimeOutline />
            </n-icon>
            <span class="stat-label">24h</span>
            <span class="stat-value">
              {{ formatNumber(group.stats_24_hour?.total_requests || 0) }}
            </span>
          </div>
          <div class="stat-item" :title="t('groupMonitor.7days')">
            <n-icon :size="10" class="stat-icon">
              <TimeOutline />
            </n-icon>
            <span class="stat-label">7d</span>
            <span class="stat-value">
              {{ formatNumber(group.stats_7_day?.total_requests || 0) }}
            </span>
          </div>
          <div class="stat-item" :title="t('groupMonitor.30days')">
            <n-icon :size="10" class="stat-icon">
              <TimeOutline />
            </n-icon>
            <span class="stat-label">30d</span>
            <span class="stat-value">
              {{ formatNumber(group.stats_30_day?.total_requests || 0) }}
            </span>
          </div>
        </div>

        <div v-if="hourlyProgress.show" class="usage-item">
          <div class="usage-header">
            <span class="usage-label">{{ t("groupMonitor.hourlyUsage") }}</span>
            <span class="usage-value">
              {{ formatNumber(usageData?.hourly_usage || 0) }} /
              {{ formatNumber(usageData?.hourly_limit || 0) }}
            </span>
          </div>
          <n-progress
            type="line"
            :percentage="hourlyProgress.percentage"
            :color="getProgressColor(hourlyProgress.percentage)"
            :height="2"
            :show-indicator="false"
          />
        </div>

        <div v-if="monthlyProgress.show" class="usage-item">
          <div class="usage-header">
            <span class="usage-label">{{ t("groupMonitor.monthlyUsage") }}</span>
            <span class="usage-value">
              {{ formatNumber(usageData?.monthly_usage || 0) }} /
              {{ formatNumber(usageData?.monthly_limit || 0) }}
            </span>
          </div>
          <n-progress
            type="line"
            :percentage="monthlyProgress.percentage"
            :color="getProgressColor(monthlyProgress.percentage)"
            :height="2"
            :show-indicator="false"
          />
        </div>
      </div>
    </template>
  </n-card>
</template>

<style scoped>
.group-monitor-card {
  cursor: pointer;
  transition: all 0.15s ease;
  background: var(--card-bg-solid);
  border: 1px solid var(--border-color-light);
  border-radius: 8px;
}

.group-monitor-card :deep(.n-card__content) {
  padding: 2px 8px 6px !important;
}

.group-monitor-card :deep(.n-card__header) {
  padding: 4px 8px 0 !important;
}

.group-monitor-card:hover {
  transform: translateY(-1px);
  box-shadow: var(--shadow-md);
}

.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 4px;
}

.header-left {
  display: flex;
  flex-direction: column;
  gap: 0;
  flex: 1;
  min-width: 0;
  margin: 0;
  padding: 0;
}

.group-name {
  font-weight: 600;
  font-size: 12px;
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  line-height: 1.2;
  margin: 0;
  padding: 0;
}

.expiry-compact {
  font-size: 8px;
  color: var(--text-tertiary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  margin: 0;
  padding: 0;
  line-height: 1;
}

.expiry-compact.expired {
  color: #d03050;
}

.header-right {
  display: flex;
  align-items: center;
  gap: 6px;
}

.action-buttons {
  display: flex;
  gap: 4px;
}

.action-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border: none;
  background: var(--bg-secondary);
  border-radius: 4px;
  cursor: pointer;
  color: var(--text-secondary);
  transition: all 0.15s ease;
}

.action-btn:hover {
  background: var(--primary-color);
  color: #fff;
}

.card-content {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

/* 统计行样式 */
.stats-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0;
  margin: 0;
}

.stat-item {
  display: flex;
  align-items: center;
  gap: 2px;
  font-size: 9px;
  color: var(--text-tertiary);
}

.stat-icon {
  opacity: 0.6;
}

.stat-label {
  font-weight: 500;
}

.stat-value {
  color: var(--text-secondary);
  font-weight: 600;
  font-family: monospace;
}

.usage-item {
  display: flex;
  flex-direction: column;
  gap: 1px;
  margin: 0;
}

.usage-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 10px;
  margin: 0;
  padding: 0;
  line-height: 1.2;
}

.usage-label {
  color: var(--text-secondary);
  font-weight: 500;
}

.usage-value {
  color: var(--text-primary);
  font-weight: 600;
  font-family: monospace;
  font-size: 10px;
}

.monitor-card.status-normal {
  border-left: 3px solid #18a058;
}

.monitor-card.status-quota-exceeded {
  border-left: 3px solid #f0a020;
  background: linear-gradient(135deg, rgba(240, 160, 32, 0.04) 0%, rgba(240, 160, 32, 0.08) 100%);
}

.monitor-card.status-expired {
  border-left: 3px solid #d03050;
  background: linear-gradient(135deg, rgba(208, 48, 80, 0.04) 0%, rgba(208, 48, 80, 0.08) 100%);
}

:root.dark .monitor-card.status-quota-exceeded {
  background: linear-gradient(135deg, rgba(240, 160, 32, 0.08) 0%, rgba(240, 160, 32, 0.12) 100%);
}

:root.dark .monitor-card.status-expired {
  background: linear-gradient(135deg, rgba(208, 48, 80, 0.08) 0%, rgba(208, 48, 80, 0.12) 100%);
}

.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.15s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
