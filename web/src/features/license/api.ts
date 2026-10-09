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
 * 授权文件解析后的明文载荷（与后端 pkg/license.LicenseData 对应）。
 * ExpireUnix 是 Unix 秒，后端用 UTC 时间生成。
 */
export interface LicenseData {
  customer: string
  expire_unix: number
}

export type LicenseStatus =
  | 'none'
  | 'valid'
  | 'grace'
  | 'expired'
  | 'forged'

export interface LicenseStatusData {
  status: LicenseStatus
  data?: LicenseData
  checked_at_unix: number
  days_left: number
  message: string
}

export interface LicenseStatusResponse {
  success: boolean
  message?: string
  data?: LicenseStatusData
}

export interface LicenseMachineCodeData {
  machine_code: string
}

export interface LicenseMachineCodeResponse {
  success: boolean
  message?: string
  data?: LicenseMachineCodeData
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
 * 获取当前服务器硬件指纹（机器码）。
 * 仅在后端显式启用机器码绑定时返回非空。
 */
export async function getMachineCode(): Promise<LicenseMachineCodeResponse> {
  const res = await api.get('/api/license/machine-code')
  return res.data
}

/**
 * 上传新的授权文件。file 为浏览器 File 对象，必须带 Authorization 头。
 * 成功时后端会立刻重载授权模块，再次校验签名 + 到期时间 + 机器码。
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
