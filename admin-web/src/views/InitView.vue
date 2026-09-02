<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'

import { ApiError } from '@infinitechance/api'

import { authErrorMessage, useAuth } from '../auth'
import AuthLayout from '../components/AuthLayout.vue'

const MIN_PASSWORD_LENGTH = 8

const auth = useAuth()
const router = useRouter()

const username = ref('admin')
const password = ref('')
const confirmPassword = ref('')
const error = ref('')
const submitting = ref(false)

async function submit(): Promise<void> {
  if (submitting.value) {
    return
  }
  const name = username.value.trim()
  if (name.length === 0) {
    error.value = '请填写用户名'
    return
  }
  if (password.value.length < MIN_PASSWORD_LENGTH) {
    error.value = `密码至少 ${MIN_PASSWORD_LENGTH} 个字符`
    return
  }
  if (password.value !== confirmPassword.value) {
    error.value = '两次输入的密码不一致'
    return
  }

  error.value = ''
  submitting.value = true
  try {
    await auth.initAdmin(name, password.value)
    await router.replace('/')
  } catch (e) {
    if (e instanceof ApiError && e.code === 'already_initialized') {
      // 初始化已完成:引导不再出现,转去登录。
      await router.replace({ name: 'login' })
      return
    }
    error.value = authErrorMessage(e)
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <AuthLayout>
    <form
      class="auth-form"
      @submit.prevent="submit"
    >
      <p class="hint">
        首次使用:请创建唯一的管理员账号。设置完成后此引导不再出现。
      </p>

      <label>
        <span>用户名</span>
        <input
          v-model="username"
          type="text"
          name="username"
          autocomplete="username"
          required
        >
      </label>
      <label>
        <span>密码(至少 8 个字符)</span>
        <input
          v-model="password"
          type="password"
          name="new-password"
          autocomplete="new-password"
          required
        >
      </label>
      <label>
        <span>确认密码</span>
        <input
          v-model="confirmPassword"
          type="password"
          name="confirm-password"
          autocomplete="new-password"
          required
        >
      </label>

      <p
        v-if="error"
        class="error"
        role="alert"
      >
        {{ error }}
      </p>

      <button
        type="submit"
        :disabled="submitting"
      >
        {{ submitting ? '创建中…' : '创建管理员并进入' }}
      </button>
    </form>
  </AuthLayout>
</template>

<style scoped src="../components/auth-form.css"></style>

<style scoped>
.hint {
  margin: 0 0 4px;
  color: #8b91a7;
  font-size: 13px;
  line-height: 1.6;
}
</style>
