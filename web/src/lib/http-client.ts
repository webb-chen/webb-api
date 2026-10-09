/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import axios, { type AxiosRequestConfig } from 'axios'
import { t } from 'i18next'

import {
  applyAuthRotation,
  clearAuthentication,
  getFreshAuthHeaders,
  refreshAuthentication,
} from '@/lib/auth-session'
import { handleServerError } from '@/lib/handle-server-error'
import {
  getServerErrorMessage,
  safeServerErrorMessage,
} from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

declare module 'axios' {
  export interface AxiosRequestConfig {
    skipBusinessError?: boolean
    skipErrorHandler?: boolean
    disableDuplicate?: boolean
    skipAuthRefresh?: boolean
    authRetry?: boolean
    acceptAuthRotation?: boolean
    singleUseAuthorization?: boolean
  }
}

export type ApiRequestConfig = AxiosRequestConfig

export const api = axios.create({
  baseURL: '',
  withCredentials: true,
  headers: {
    // no-store forbids storage; no-cache also revalidates any older cached response.
    'Cache-Control': 'no-cache, no-store',
  },
})

const inFlightGet = new Map<string, Promise<unknown>>()
const originalGet = api.get.bind(api)

api.get = ((url: string, config: ApiRequestConfig = {}) => {
  if (config.disableDuplicate) return originalGet(url, config)

  const params = config.params ? JSON.stringify(config.params) : '{}'
  const sessionSID = useAuthStore.getState().auth.session?.sid || 'anonymous'
  const key = `${sessionSID}:${url}?${params}`
  const existingRequest = inFlightGet.get(key)
  if (existingRequest) return existingRequest

  const request = originalGet(url, config).finally(() => {
    inFlightGet.delete(key)
  })
  inFlightGet.set(key, request)
  return request
}) as typeof api.get

function redirectToSignIn(): void {
  if (
    typeof window !== 'undefined' &&
    window.location.pathname !== '/sign-in'
  ) {
    window.location.replace('/sign-in')
  }
}

// 授权失效（403 + license 错误码）时，跳转到授权管理页。
// 业务侧 403（如权限不足）不会带 LIC_* 错误码，不会触发跳转。
function redirectToLicensePage(message?: string): void {
  if (typeof window === 'undefined') return
  if (window.location.pathname === '/license') return
  window.location.replace('/license')
}

api.interceptors.response.use(
  (response) => {
    if (response.config.acceptAuthRotation && response.data?.success === true) {
      applyAuthRotation(response.data.data)
    }

    return response
  },
  async (error) => {
    const config = error?.config as ApiRequestConfig | undefined
    const skipErrorHandler = config?.skipErrorHandler
    const status = error?.response?.status

    // 授权（License）失效拦截：
    // 后端 LicenseAuth 中间件对业务路径返回 403 + 错误码 LIC_EXPIRED / LIC_FORGED / LIC_MISSING / LIC_MACHINE_CODE
    // 前端收到后直接跳到 /license 页，让用户看到「上传新授权文件」入口。
    // 注意 /api/license/upload 与 /api/license/status 是恢复通路，本身不会 403。
    const licCode = (error?.response?.data as { code?: string })?.code
    if (status === 403 && typeof licCode === 'string' && licCode.startsWith('LIC_')) {
      if (!skipErrorHandler) {
        const licMsg =
          (error?.response?.data as { message?: string })?.message ||
          t('License is no longer valid. Please upload a new license file.')
        handleServerError({
          message: licMsg,
          [safeServerErrorMessage]: true,
          cause: error,
        })
      }
      redirectToLicensePage(licCode)
      // 不继续抛出，避免普通错误处理再次提示
      return Promise.reject(error)
    }

    if (status === 401) {
      if (config && !config.skipAuthRefresh && !config.authRetry) {
        config.authRetry = true
        const outcome = await refreshAuthentication()
        if (outcome.kind === 'authenticated') {
          const token = useAuthStore.getState().auth.accessToken
          if (token) {
            config.headers = {
              ...config.headers,
              Authorization: `Bearer ${token}`,
            }
          }
          return api.request(config)
        }

        if (outcome.kind === 'anonymous' || outcome.kind === 'out_of_sync') {
          if (!skipErrorHandler) {
            handleServerError({
              message: t('Session expired!'),
              [safeServerErrorMessage]: true,
              cause: error,
            })
          }
          redirectToSignIn()
        }
      } else if (config?.authRetry) {
        clearAuthentication(false)
        if (!skipErrorHandler) {
          handleServerError({
            message: t('Session expired!'),
            [safeServerErrorMessage]: true,
            cause: error,
          })
        }
        redirectToSignIn()
      } else if (!skipErrorHandler) {
        handleServerError({
          message: t('Session expired!'),
          [safeServerErrorMessage]: true,
          cause: error,
        })
      }
    }
    if (axios.isAxiosError(error)) error.message = getServerErrorMessage(error)
    throw error
  }
)

api.interceptors.request.use(async (config) => {
  if (config.singleUseAuthorization || config.headers.has('X-Security-Proof')) {
    // Refresh before spending a proof/flow, never by replaying its request.
    config.skipAuthRefresh = true
    try {
      const headers = await getFreshAuthHeaders()
      for (const [name, value] of Object.entries(headers)) {
        config.headers.set(name, value)
      }
    } catch (error) {
      throw axios.AxiosError.from(error, undefined, config)
    }
    return config
  }
  const accessToken = useAuthStore.getState().auth.accessToken
  if (accessToken) {
    config.headers.Authorization = `Bearer ${accessToken}`
  }
  return config
})
