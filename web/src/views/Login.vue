<script setup lang="ts">
import AppFooter from "@/components/AppFooter.vue";
import LanguageSelector from "@/components/LanguageSelector.vue";
import { useAuthService } from "@/services/auth";
import { LockClosedSharp } from "@vicons/ionicons5";
import { NButton, NCard, NIcon, NInput, NSpace, useMessage } from "naive-ui";
import { ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";

const authKey = ref("");
const loading = ref(false);
const router = useRouter();
const message = useMessage();
const { login } = useAuthService();
const { t } = useI18n();

const handleLogin = async () => {
  if (!authKey.value) {
    message.error(t("login.authKeyRequired"));
    return;
  }
  loading.value = true;
  const success = await login(authKey.value);
  loading.value = false;
  if (success) {
    router.push("/");
  }
};
</script>

<template>
  <div class="login-container">
    <!-- 语言切换器 -->
    <div class="language-selector-wrapper">
      <language-selector />
    </div>
    <div class="login-background">
      <div class="login-decoration" />
      <div class="login-decoration-2" />
    </div>

    <div class="login-content">
      <!-- <div class="login-header">
        <h1 class="login-title">{{ t("login.title") }}</h1>
        <p class="login-subtitle">{{ t("login.subtitle") }}</p>
      </div> -->

      <n-card class="login-card modern-card" :bordered="false">
        <template #header>
          <div class="card-header">
            <!-- <h2 class="card-title">{{ t("login.welcome") }}</h2>
            <p class="card-subtitle">{{ t("login.welcomeDesc") }}</p> -->
          </div>
        </template>

        <n-space vertical size="large">
          <n-input
            v-model:value="authKey"
            type="password"
            size="large"
            :placeholder="t('login.authKeyPlaceholder')"
            class="modern-input"
            @keyup.enter="handleLogin"
          >
            <template #prefix>
              <n-icon :component="LockClosedSharp" />
            </template>
          </n-input>

          <n-button
            class="login-btn modern-button"
            type="primary"
            size="large"
            block
            @click="handleLogin"
            :loading="loading"
            :disabled="loading"
          >
            <template v-if="!loading">
              <span>{{ t("login.loginButton") }}</span>
            </template>
          </n-button>
        </n-space>
      </n-card>
    </div>
  </div>
  <app-footer />
</template>

<style scoped>
.language-selector-wrapper {
  position: absolute;
  top: 24px;
  right: 24px;
  z-index: 10;
}

.login-container {
  min-height: calc(100vh - 52px);
  display: flex;
  justify-content: center;
  align-items: center;
  position: relative;
  overflow: hidden;
  padding: 24px;
}

.login-background {
  position: absolute;
  top: 0;
  left: 0;
  width: 100%;
  height: 100%;
  z-index: 0;
}

.login-decoration {
  position: absolute;
  top: -50%;
  right: -20%;
  width: 800px;
  height: 800px;
  background: var(--primary-gradient);
  border-radius: 50%;
  opacity: 0.1;
  animation: float 6s ease-in-out infinite;
}

.login-decoration-2 {
  position: absolute;
  bottom: -50%;
  left: -20%;
  width: 600px;
  height: 600px;
  background: var(--secondary-gradient);
  border-radius: 50%;
  opacity: 0.08;
  animation: float 8s ease-in-out infinite reverse;
}

@keyframes float {
  0%,
  100% {
    transform: translateY(0px) rotate(0deg);
  }
  50% {
    transform: translateY(-20px) rotate(5deg);
  }
}

.login-content {
  position: relative;
  z-index: 1;
  width: 100%;
  max-width: 420px;
  padding: 0 20px;
}

.login-header {
  text-align: center;
  margin-bottom: 40px;
}

.login-title {
  font-size: 2.5rem;
  font-weight: 700;
  background: var(--primary-gradient);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  margin-bottom: 8px;
  letter-spacing: -0.5px;
}

.login-subtitle {
  font-size: 1.1rem;
  color: var(--text-secondary);
  margin: 0;
  font-weight: 500;
}

.login-card {
  backdrop-filter: blur(20px);
  border: 1px solid var(--border-color-light);
}

.card-header {
  text-align: center;
  padding-bottom: 8px;
}

.card-title {
  font-size: 1.5rem;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0 0 8px 0;
}

.card-subtitle {
  font-size: 0.95rem;
  color: var(--text-secondary);
  margin: 0;
}

.login-btn {
  /* 绿色玻璃效果 */
  background: linear-gradient(
    135deg,
    rgba(34, 197, 94, 0.9) 0%,
    rgba(22, 163, 74, 0.85) 50%,
    rgba(21, 128, 61, 0.9) 100%
  );
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
  border: 1px solid rgba(255, 255, 255, 0.3);
  box-shadow:
    0 4px 16px rgba(34, 197, 94, 0.3),
    inset 0 1px 0 rgba(255, 255, 255, 0.2),
    inset 0 -1px 0 rgba(0, 0, 0, 0.1);
  font-weight: 600;
  letter-spacing: 0.5px;
  height: 48px;
  font-size: 1rem;
  position: relative;
  overflow: hidden;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}

/* 添加光泽效果 */
.login-btn::before {
  content: "";
  position: absolute;
  top: 0;
  left: -100%;
  width: 100%;
  height: 100%;
  background: linear-gradient(90deg, transparent, rgba(255, 255, 255, 0.3), transparent);
  transition: left 0.5s;
}

.login-btn:hover::before {
  left: 100%;
}

.login-btn:hover {
  /* 悬停时的绿色渐变 */
  background: linear-gradient(
    135deg,
    rgba(22, 163, 74, 0.95) 0%,
    rgba(21, 128, 61, 0.9) 50%,
    rgba(20, 83, 45, 0.95) 100%
  );
  transform: translateY(-2px) scale(1.02);
  box-shadow:
    0 12px 32px rgba(34, 197, 94, 0.45),
    0 6px 16px rgba(34, 197, 94, 0.3),
    inset 0 1px 0 rgba(255, 255, 255, 0.3),
    inset 0 -1px 0 rgba(0, 0, 0, 0.1);
  border-color: rgba(255, 255, 255, 0.4);
}

/* 点击效果 */
.login-btn:active {
  transform: translateY(0) scale(0.98);
  box-shadow:
    0 4px 12px rgba(34, 197, 94, 0.3),
    inset 0 2px 4px rgba(0, 0, 0, 0.15);
}

/* 加载状态 */
.login-btn:deep(.n-button__icon) {
  color: rgba(255, 255, 255, 0.9);
}

/* 禁用状态 */
.login-btn:deep(.n-button--disabled) {
  opacity: 0.6;
  filter: grayscale(0.3);
}

:deep(.n-input) {
  --n-border-radius: 12px;
  --n-height: 48px;
}

:deep(.n-input__input-el) {
  font-size: 1rem;
}

:deep(.n-input__prefix) {
  color: var(--text-secondary);
}

:deep(.n-card-header) {
  padding-bottom: 16px;
}

:deep(.n-card__content) {
  padding-top: 0;
}

/* 暗黑模式适配 */
:root.dark .login-decoration {
  opacity: 0.05;
}

:root.dark .login-decoration-2 {
  opacity: 0.03;
}

:root.dark .login-card {
  background: var(--card-bg-solid);
  border: 1px solid rgba(255, 255, 255, 0.08);
}

:root.dark .login-btn {
  background: linear-gradient(
    135deg,
    rgba(34, 197, 94, 0.85) 0%,
    rgba(22, 163, 74, 0.8) 50%,
    rgba(21, 128, 61, 0.85) 100%
  );
  border: 1px solid rgba(74, 222, 128, 0.3);
  box-shadow:
    0 4px 16px rgba(34, 197, 94, 0.25),
    inset 0 1px 0 rgba(255, 255, 255, 0.15);
}

:root.dark .login-btn:hover {
  background: linear-gradient(
    135deg,
    rgba(22, 163, 74, 0.9) 0%,
    rgba(21, 128, 61, 0.85) 50%,
    rgba(20, 83, 45, 0.9) 100%
  );
  box-shadow:
    0 12px 32px rgba(34, 197, 94, 0.4),
    0 6px 16px rgba(34, 197, 94, 0.25),
    inset 0 1px 0 rgba(255, 255, 255, 0.2);
  border-color: rgba(74, 222, 128, 0.4);
}

:root.dark .login-btn:active {
  box-shadow:
    0 4px 12px rgba(34, 197, 94, 0.2),
    inset 0 2px 4px rgba(0, 0, 0, 0.2);
}
</style>
