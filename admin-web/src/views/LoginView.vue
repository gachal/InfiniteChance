<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { UnauthorizedError } from '@infinitechance/api'

import { authErrorMessage, useAuth } from '../auth'
import AuthLayout from '../components/AuthLayout.vue'

const auth = useAuth()
const route = useRoute()
const router = useRouter()

const username = ref('')
const password = ref('')
const error = ref('')
const submitting = ref(false)

async function submit(): Promise<void> {
  if (submitting.value) {
    return
  }
  error.value = ''
  submitting.value = true
  try {
    await auth.login(username.value.trim(), password.value)
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/'
    await router.replace(redirect)
  } catch (e) {
    if (e instanceof UnauthorizedError && e.code === 'invalid_credentials') {
      error.value = '用户名或密码错误'
    } else {
      error.value = authErrorMessage(e)
    }
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
      <label>
        <span>用户名</span>
        <input
          v-model="username"
          type="text"
          name="username"
          autocomplete="username"
          autofocus
          required
        >
      </label>
      <label>
        <span>密码</span>
        <input
          v-model="password"
          type="password"
          name="password"
          autocomplete="current-password"
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
        {{ submitting ? '登录中…' : '登录' }}
      </button>
    </form>
  </AuthLayout>
</template>

<style scoped src="../components/auth-form.css"></style>
