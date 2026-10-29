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
import { api } from '@/lib/api'

/**
 * 统一授权平台（ULP）License Format v1 的载荷字段。
 * 与后端 `pkg/license.Payload` 一一对应；永久授权的 expire_time 为 null。
 */
export interface LicensePayload {
  license_id: string
  product: string
  product_display: string
  customer: string
  customer_contact?: string
  issued_at: string | null
  expire_time: string | null
  grace_days: number
  machine_code: string
  instance_id?: string
  features?: Record<string, unknown>
  permanent: boolean
}

/**
 * 统一状态码词表（《统一授权文件格式规范》§7）。
 * 前端展示与跳转判据都必须使用这套字符串。
 */
export type LicenseCode =
  | 'ok'
  | 'license_grace'
  | 'license_disabled'
  | 'license_not_init'
  | 'license_missing'
  | 'license_malformed'
  | 'license_bad_signature'
  | 'license_expired'
  | 'license_clock_rollback'
  | 'license_machine_mismatch'
  | 'license_product_mismatch'
  | 'license_no_public_key'

export interface LicenseStatusData {
  code: string
  valid: boolean
  message: string
  in_grace: boolean
  remaining_days: number
  checked_at: string
  license: LicensePayload | null
}

export interface LicenseStatusResponse extends LicenseStatusData {
  success: boolean
  data: LicenseStatusData
}

export interface LicenseMachineCodeData {
  machine_code: string
}

export interface LicenseMachineCodeResponse {
  success: boolean
  code: string
  machine_code: string
  data: LicenseMachineCodeData
}

/**
 * 获取当前授权状态。未登录也能拿到（前端侧边栏/登录页提示条用）。
 */
export async function getLicenseStatus(): Promise<LicenseStatusResponse> {
  const res = await api.get('/api/license/status', {
    // 授权状态必须每次重新拿，避免本地缓存的过期数据误导用户
    disableDuplicate: true,
  })
  return res.data
}

/**
 * 获取当前服务器统一机器码（ULP-LICENSE-V1 算法）。
 */
export async function getMachineCode(): Promise<LicenseMachineCodeResponse> {
  const res = await api.get('/api/license/machine-code')
  return res.data
}

/**
 * 上传新的授权文件。file 为浏览器 File 对象。
 * 成功时后端会立刻重载授权模块，再次校验签名 + 产品 + 机器码 + 到期时间。
 * 校验失败时后端仍返回 HTTP 200，用 `code`/`message` 表达失败原因。
 */
export async function uploadLicense(
  file: File
): Promise<LicenseStatusResponse> {
  const form = new FormData()
  form.append('file', file)
  const res = await api.post('/api/license/upload', form, {
    headers: { 'Content-Type': 'multipart/form-data' },
  })
  return res.data
}

/**
 * 手动重新读取授权文件（一般用于运维排查，业务侧默认走后台复检协程即可）。
 */
export async function reloadLicense(): Promise<LicenseStatusResponse> {
  const res = await api.post('/api/license/reload')
  return res.data
}
