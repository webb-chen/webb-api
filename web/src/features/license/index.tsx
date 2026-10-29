// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2023-2026 QuantumNous
//
// 授权管理页面——展示统一授权平台（ULP）授权状态 + 上传授权文件 + 手动重检。
// 状态码使用《统一授权文件格式规范》§7 的词表，与后端逐字一致。

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { AuthLayout } from '@/features/auth/auth-layout'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Badge, badgeVariants } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { cn } from '@/lib/utils'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

import {
  getLicenseStatus,
  getMachineCode,
  reloadLicense,
  uploadLicense,
} from './api'

type StatusTone = 'success' | 'warning' | 'destructive' | 'muted'

function toneForCode(code: string | undefined): StatusTone {
  switch (code) {
    case 'ok':
      return 'success'
    case 'license_grace':
    case 'license_disabled':
      return 'warning'
    case 'license_missing':
    case 'license_malformed':
    case 'license_bad_signature':
    case 'license_expired':
    case 'license_clock_rollback':
    case 'license_machine_mismatch':
    case 'license_product_mismatch':
    case 'license_not_init':
    case 'license_no_public_key':
      return 'destructive'
    default:
      return 'muted'
  }
}

function badgeToneClass(tone: StatusTone): string {
  switch (tone) {
    case 'success':
      return badgeVariants({ variant: 'default' })
    case 'warning':
      return badgeVariants({ variant: 'secondary' })
    case 'destructive':
      return badgeVariants({ variant: 'destructive' })
    default:
      return badgeVariants({ variant: 'outline' })
  }
}

function formatDateTime(value: string | null | undefined): string {
  if (!value) return '—'
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return value
  return d.toLocaleString()
}

export function LicenseManagement() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const statusQ = useQuery({
    queryKey: ['license', 'status'],
    queryFn: async () => {
      const res = await getLicenseStatus()
      return requireServerSuccess(res)
    },
  })
  const machineQ = useQuery({
    queryKey: ['license', 'machine-code'],
    queryFn: async () => {
      const res = await getMachineCode()
      return requireServerSuccess(res)
    },
  })

  const upload = useMutation({
    mutationFn: (file: File) => uploadLicense(file),
    onSuccess: (res) => {
      if (!res.valid) {
        toast.error(res.message || t('Upload failed'))
        return
      }
      toast.success(t('License has been reloaded'))
      if (res.code === 'license_grace') {
        toast.warning(t('License has expired and is now in the grace period'))
      }
      void queryClient.invalidateQueries({ queryKey: ['license'] })
    },
    onError: (err) => {
      handleServerError(err, t('Upload failed'))
    },
  })

  const reload = useMutation({
    mutationFn: async () => {
      const res = await reloadLicense()
      return requireServerSuccess(res)
    },
    onSuccess: () => {
      toast.success(t('License has been reloaded'))
      void queryClient.invalidateQueries({ queryKey: ['license'] })
    },
    onError: (err) => {
      handleServerError(err, t('Failed to load license status'))
    },
  })

  const statusData = statusQ.data
  const license = statusData?.license
  const machineCode = machineQ.data?.machine_code ?? license?.machine_code
  const code = statusData?.code
  const daysLeft = statusData?.remaining_days ?? 0

  const statusLabel = (() => {
    switch (code) {
      case 'ok':
        return t('License is valid')
      case 'license_grace':
        return t('License has expired and is now in the grace period')
      case 'license_disabled':
        return t('License verification is disabled')
      case 'license_not_init':
        return t('License module is not ready')
      case 'license_missing':
        return t('No license file found')
      case 'license_malformed':
        return t('License file is malformed')
      case 'license_bad_signature':
        return t('License signature verification failed')
      case 'license_expired':
        return t('License has expired')
      case 'license_clock_rollback':
        return t('System clock rollback detected')
      case 'license_machine_mismatch':
        return t('License is bound to another machine')
      case 'license_product_mismatch':
        return t('License belongs to another product')
      case 'license_no_public_key':
        return t('License public key is not configured')
      default:
        return t('License status unknown')
    }
  })()

  const tone = toneForCode(code)

  return (
    <AuthLayout>
      <div className='space-y-6'>
        <div>
          <h1 className='text-2xl font-semibold'>{t('License management')}</h1>
          <p className='mt-1 text-sm text-muted-foreground'>
            {t(
              'View the current license status and upload a new license file to renew.'
            )}
          </p>
        </div>

        {statusQ.isLoading && (
          <LoadingState label={t('Loading license status…')} />
        )}

        {statusQ.isError && (
          <ErrorState
            title={t('Failed to load license status')}
            message={t('Please check the server connection and try again.')}
          />
        )}

        {statusData && (
          <Card>
            <CardHeader>
              <CardTitle>{t('License status')}</CardTitle>
              <CardDescription>
                {statusData.message || t('No additional message')}
              </CardDescription>
            </CardHeader>
            <CardContent className='space-y-3'>
              <div className='flex flex-wrap items-center gap-2'>
                <span>{statusLabel}</span>
                <Badge
                  className={cn(
                    badgeToneClass(tone),
                    code === 'license_grace' &&
                      'border-amber-500/50 bg-amber-500/15 text-amber-500'
                  )}
                >
                  {license?.permanent
                    ? t('Permanent license')
                    : t('Days left: {{days}}', { days: daysLeft })}
                </Badge>
              </div>

              <dl className='grid grid-cols-[120px_1fr] gap-x-4 gap-y-1 text-sm'>
                <dt className='text-muted-foreground'>{t('Product')}</dt>
                <dd>{license?.product_display || license?.product || '—'}</dd>
                <dt className='text-muted-foreground'>{t('Customer')}</dt>
                <dd>{license?.customer || '—'}</dd>
                <dt className='text-muted-foreground'>{t('Expire time')}</dt>
                <dd>
                  {license?.permanent
                    ? t('Permanent license')
                    : formatDateTime(license?.expire_time)}
                </dd>
                <dt className='text-muted-foreground'>{t('Checked at')}</dt>
                <dd>{formatDateTime(statusData.checked_at)}</dd>
              </dl>

              {machineCode && (
                <div className='space-y-1'>
                  <Label className='text-xs text-muted-foreground'>
                    {t('Machine code')}
                  </Label>
                  <div className='flex items-center gap-2'>
                    <code className='rounded bg-muted px-2 py-1 font-mono text-xs'>
                      {machineCode}
                    </code>
                    <Button
                      variant='ghost'
                      size='sm'
                      onClick={() => {
                        void navigator.clipboard.writeText(machineCode)
                        toast.success(t('Copied'))
                      }}
                    >
                      {t('Copy')}
                    </Button>
                  </div>
                  <p className='text-xs text-muted-foreground'>
                    {t(
                      'Send this code to the vendor to request a license file. The code binds the license to this server.'
                    )}
                  </p>
                </div>
              )}

              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={reload.isPending}
                onClick={() => reload.mutate()}
              >
                {reload.isPending ? t('Checking…') : t('Re-check license')}
              </Button>
            </CardContent>
          </Card>
        )}

        {/* 上传授权文件——始终显示，未登录也可访问 */}
        <Card>
          <CardHeader>
            <CardTitle>{t('Upload license file')}</CardTitle>
            <CardDescription>
              {t(
                'Drop a .lic file issued by the vendor. The backend verifies the RSA signature and expiry immediately.'
              )}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <form
              className='space-y-3'
              onSubmit={(e) => {
                e.preventDefault()
                const input = e.currentTarget.querySelector(
                  'input[type=file]'
                ) as HTMLInputElement
                if (!input?.files?.[0]) {
                  toast.error(t('Please select a license file first'))
                  return
                }
                upload.mutate(input.files[0])
              }}
            >
              <div className='space-y-1'>
                <Label htmlFor='license-file'>{t('License file')}</Label>
                <Input
                  id='license-file'
                  type='file'
                  accept='.lic,application/json'
                  className='cursor-pointer'
                  aria-describedby='license-file-desc'
                />
                <p
                  id='license-file-desc'
                  className='text-xs text-muted-foreground'
                >
                  {t(
                    'Only .lic files issued by the vendor are accepted. The file contains the license payload and the RSA signature; any tampering will cause verification to fail.'
                  )}
                </p>
              </div>
              <Button
                type='submit'
                variant='default'
                className={cn(buttonVariants())}
                disabled={upload.isPending}
              >
                {upload.isPending ? t('Uploading…') : t('Upload & verify')}
              </Button>
            </form>
          </CardContent>
        </Card>
      </div>
    </AuthLayout>
  )
}
