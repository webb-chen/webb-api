// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2023-2026 QuantumNous
//
// 授权管理页面——展示授权状态 + 上传授权文件。

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

import { getLicenseStatus, getMachineCode, uploadLicense } from './api'

type StatusTone = 'success' | 'warning' | 'destructive' | 'muted'

function toneForStatus(code: string | undefined): StatusTone {
  if (code === 'valid') return 'success'
  if (code === 'grace') return 'warning'
  if (code === 'license_missing' || code === 'expired' || code === 'forged')
    return 'destructive'
  return 'muted'
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

function formatExpire(value: string | undefined): string {
  if (!value) return '—'
  const d = new Date(value)
  if (isNaN(d.getTime())) return value
  return d.toLocaleString()
}

export function LicenseManagement() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const statusQ = useQuery({
    queryKey: ['license', 'status'],
    queryFn: async () => {
      const res = await getLicenseStatus()
      return requireServerSuccess(res)?.data
    },
  })
  const machineQ = useQuery({
    queryKey: ['license', 'machine-code'],
    queryFn: async () => {
      const res = await getMachineCode()
      return requireServerSuccess(res)?.data
    },
  })

  const upload = useMutation({
    mutationFn: async (file: File) => {
      const res = await uploadLicense(file)
      return requireServerSuccess(res)
    },
    onSuccess: () => {
      toast.success(t('License has been reloaded'))
      void queryClient.invalidateQueries({ queryKey: ['license'] })
    },
    onError: (err) => {
      handleServerError(err, t('Upload failed'))
    },
  })

  // API 响应结构: { data: { code, message, valid, remaining_days, ... }, success, message }
  const statusData = statusQ.data?.data
  const machineCode = machineQ.data?.data?.machine_code
  const daysLeft = statusData?.remaining_days ?? 0
  const code = statusData?.code

  const statusLabel = (() => {
    switch (code) {
      case 'valid':
        return t('License is valid')
      case 'grace':
        return t('License has expired and is now in the grace period')
      case 'license_missing':
        return t('No license file found')
      case 'expired':
        return t('License has expired')
      case 'forged':
        return t('License is forged or has been tampered with')
      default:
        return t('License status unknown')
    }
  })()

  const tone = toneForStatus(code)

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
              <div className='flex items-center gap-2'>
                <span>{statusLabel}</span>
                <Badge
                  className={cn(
                    badgeToneClass(tone),
                    code === 'grace' &&
                      'border-amber-500/50 bg-amber-500/15 text-amber-500'
                  )}
                >
                  {t('Days left: {{days}}', { days: daysLeft })}
                </Badge>
              </div>

              <dl className='grid grid-cols-[120px_1fr] gap-x-4 gap-y-1 text-sm'>
                <dt className='text-muted-foreground'>{t('Checked at')}</dt>
                <dd>{formatExpire(statusData.checked_at)}</dd>
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
                        navigator.clipboard.writeText(machineCode)
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
                  accept='.lic'
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
                {upload.isPending
                  ? t('Uploading…')
                  : t('Upload & verify')}
              </Button>
            </form>
          </CardContent>
        </Card>
      </div>
    </AuthLayout>
  )
}
