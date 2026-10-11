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
import { zodResolver } from '@hookform/resolvers/zod'
import { useQueries, useQueryClient } from '@tanstack/react-query'
import { Plus, Trash2, Edit3, Brain, Play } from 'lucide-react'
import { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { getChannel } from '@/features/channels/api'
import { channelsQueryKeys } from '@/features/channels/lib/channel-actions'
import { api } from '@/lib/api'

import {
  SettingsControlGroup,
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { getOptionValue } from '../hooks/use-system-options'
import { useUpdateOption } from '../hooks/use-update-option'
import type { SystemOptionsResponse } from '../types'
import { safeNumberFieldProps } from '../utils/numeric-field'
import { ModelBindingDialog } from './degradation-binding-dialog'
import { AddTargetDialog } from './degradation-target-dialog'

export type ProbeConfig = {
  id: string
  name: string
  system_prompt?: string
  prompt: string
  keywords: string[]
  exclude_words?: string[]
  match_rule: 'any' | 'all' | 'regex'
  case_sensitive: boolean
  max_tokens: number
  temperature?: number
}

export type ModelProbeBinding = {
  model_name: string
  enabled: boolean
  probe_ids?: string[]
  reasoning_effort?: string
  thinking_budget?: number
}

export type DetectionTarget = {
  channel_id: number
  channel_name?: string
  models?: string[]
  bindings?: ModelProbeBinding[]
}

const degradationCheckSchema = z.object({
  degradation_setting: z.object({
    enabled: z.boolean(),
    interval_minutes: z.number().int().min(1).max(1440),
    concurrency: z.number().int().min(1).max(32),
    channel_jitter_ms: z.number().int().min(50).max(5000),
    timeout_seconds: z.number().int().min(5).max(180),
    retention_days: z.number().int().min(1).max(90),
    public_mask_topology: z.boolean(),
    targets: z.string(),
    probes: z.string(),
  }),
})

type DegradationCheckFormValues = z.infer<typeof degradationCheckSchema>

type FlatDegradationCheckDefaults = {
  [Key in keyof DegradationCheckFormValues['degradation_setting'] as `degradation_setting.${Key}`]: DegradationCheckFormValues['degradation_setting'][Key]
}

type DegradationCheckSectionProps = {
  defaultValues: FlatDegradationCheckDefaults
}

export function DegradationCheckSection(props: DegradationCheckSectionProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const updateOption = useUpdateOption()
  // Dotted form names are nested paths in react-hook-form, so defaults and
  // validation must use the same shape before converting back to option keys.
  const defaultValues = useMemo<DegradationCheckFormValues>(
    () => ({
      degradation_setting: {
        enabled: props.defaultValues['degradation_setting.enabled'],
        interval_minutes:
          props.defaultValues['degradation_setting.interval_minutes'],
        concurrency: props.defaultValues['degradation_setting.concurrency'],
        channel_jitter_ms:
          props.defaultValues['degradation_setting.channel_jitter_ms'],
        timeout_seconds:
          props.defaultValues['degradation_setting.timeout_seconds'],
        retention_days:
          props.defaultValues['degradation_setting.retention_days'],
        public_mask_topology:
          props.defaultValues['degradation_setting.public_mask_topology'],
        targets: props.defaultValues['degradation_setting.targets'],
        probes: props.defaultValues['degradation_setting.probes'],
      },
    }),
    [props.defaultValues]
  )

  const form = useForm<DegradationCheckFormValues>({
    resolver: zodResolver(degradationCheckSchema),
    defaultValues,
  })

  const setFieldValue = form.setValue
  const { isDirty, isSubmitting } = form.formState
  const lastSerializedDefaults = useRef<string | null>(null)

  useEffect(() => {
    const serializedDefaults = JSON.stringify(defaultValues)
    if (serializedDefaults === lastSerializedDefaults.current) return
    lastSerializedDefaults.current = serializedDefaults
    // Per-option refetches contain partial saves; never replace a draft with
    // them, or replay them once the submission becomes clean.
    if (isDirty || isSubmitting) return
    form.reset(defaultValues)
  }, [defaultValues, form, isDirty, isSubmitting])

  const probesValue = form.watch('degradation_setting.probes')
  const targetsValue = form.watch('degradation_setting.targets')

  // 解析并管理探针与目标。解析失败时保留空列表用于展示，但标记无效并禁止
  // 保存，避免把损坏的 JSON 静默覆写为空配置
  const parsedProbes = useMemo<{ data: ProbeConfig[]; valid: boolean }>(() => {
    try {
      const parsed = JSON.parse(probesValue || '[]')
      return {
        data: Array.isArray(parsed) ? (parsed as ProbeConfig[]) : [],
        valid: Array.isArray(parsed),
      }
    } catch {
      return { data: [], valid: false }
    }
  }, [probesValue])
  const probes = parsedProbes.data

  const parsedTargets = useMemo<{
    data: DetectionTarget[]
    valid: boolean
  }>(() => {
    try {
      const parsed = JSON.parse(targetsValue || '[]')
      return {
        data: Array.isArray(parsed) ? (parsed as DetectionTarget[]) : [],
        valid: Array.isArray(parsed),
      }
    } catch {
      return { data: [], valid: false }
    }
  }, [targetsValue])
  const targets = parsedTargets.data
  const configInvalid = !parsedProbes.valid || !parsedTargets.valid
  const allModelTargets = targets.filter((target) => !target.models?.length)
  const channelQueries = useQueries({
    queries: allModelTargets.map((target) => ({
      queryKey: channelsQueryKeys.detail(target.channel_id),
      queryFn: async () => {
        const response = await getChannel(target.channel_id)
        if (!response.success || !response.data) {
          throw new Error(response.message || t('Failed to load'))
        }
        return response
      },
    })),
  })

  // 弹窗状态
  const [probeModalOpen, setProbeModalOpen] = useState(false)
  const [editingProbe, setEditingProbe] = useState<ProbeConfig | null>(null)
  const [probePendingDelete, setProbePendingDelete] = useState<{
    probe: ProbeConfig
    affected: string[]
  } | null>(null)

  const [targetModalOpen, setTargetModalOpen] = useState(false)

  const [bindingModalOpen, setBindingModalOpen] = useState(false)
  const [activeTargetIndex, setActiveTargetIndex] = useState<number>(-1)
  const [activeModelName, setActiveModelName] = useState<string>('')
  // 每次打开绑定弹窗时递增，驱动 ModelBindingDialog 重挂载并重新初始化
  const [bindingSession, setBindingSession] = useState(0)

  const [runningAll, setRunningAll] = useState(false)
  const [testingModelKey, setTestingModelKey] = useState<string | null>(null)

  const handleSaveProbe = useCallback(
    (probe: ProbeConfig) => {
      const current = [...probes]
      const idx = current.findIndex((p) => p.id === probe.id)
      if (idx >= 0) {
        current[idx] = probe
      } else {
        current.push(probe)
      }
      setFieldValue('degradation_setting.probes', JSON.stringify(current), {
        shouldDirty: true,
      })
      setProbeModalOpen(false)
      setEditingProbe(null)
    },
    [probes, setFieldValue]
  )

  // 删除探针前检查显式绑定：命中 probe_ids 的绑定在删除后将不再执行该探针，
  // 需要在确认弹窗中明示，避免相关模型静默停止巡检。
  const handleDeleteProbeClick = useCallback(
    (probe: ProbeConfig) => {
      const affected: string[] = []
      for (const target of targets) {
        for (const binding of target.bindings || []) {
          if (binding.probe_ids?.includes(probe.id)) {
            affected.push(
              `${target.channel_name || t('Channel #{{id}}', { id: target.channel_id })} / ${binding.model_name}`
            )
          }
        }
      }
      setProbePendingDelete({ probe, affected })
    },
    [t, targets]
  )

  const handleConfirmDeleteProbe = useCallback(() => {
    if (!probePendingDelete) return
    const updated = probes.filter((p) => p.id !== probePendingDelete.probe.id)
    setFieldValue('degradation_setting.probes', JSON.stringify(updated), {
      shouldDirty: true,
    })
    setProbePendingDelete(null)
  }, [probes, probePendingDelete, setFieldValue])

  const handleAddTarget = useCallback(
    (newTarget: DetectionTarget) => {
      const current = [...targets, newTarget]
      setFieldValue('degradation_setting.targets', JSON.stringify(current), {
        shouldDirty: true,
      })
    },
    [setFieldValue, targets]
  )

  const handleDeleteTarget = useCallback(
    (targetIdx: number) => {
      const current = targets.filter((_, idx) => idx !== targetIdx)
      setFieldValue('degradation_setting.targets', JSON.stringify(current), {
        shouldDirty: true,
      })
    },
    [setFieldValue, targets]
  )

  const handleToggleModelEnabled = useCallback(
    (targetIdx: number, modelName: string, enabled: boolean) => {
      const current = [...targets]
      const target = current[targetIdx]
      if (!target) return
      const bindings = [...(target.bindings || [])]
      const bIdx = bindings.findIndex((b) => b.model_name === modelName)
      if (bIdx >= 0) {
        bindings[bIdx] = { ...bindings[bIdx], enabled }
      } else {
        bindings.push({ model_name: modelName, enabled })
      }
      target.bindings = bindings
      current[targetIdx] = target
      setFieldValue('degradation_setting.targets', JSON.stringify(current), {
        shouldDirty: true,
      })
    },
    [setFieldValue, targets]
  )

  const handleOpenBindingDialog = useCallback(
    (targetIdx: number, modelName: string) => {
      setActiveTargetIndex(targetIdx)
      setActiveModelName(modelName)
      setBindingSession((session) => session + 1)
      setBindingModalOpen(true)
    },
    []
  )

  const handleSaveBinding = useCallback(
    (binding: ModelProbeBinding) => {
      const current = [...targets]
      const target = current[activeTargetIndex]
      if (!target) return
      const bindings = [...(target.bindings || [])]
      const bIdx = bindings.findIndex((b) => b.model_name === activeModelName)
      if (bIdx >= 0) {
        bindings[bIdx] = binding
      } else {
        bindings.push(binding)
      }
      target.bindings = bindings
      current[activeTargetIndex] = target
      setFieldValue('degradation_setting.targets', JSON.stringify(current), {
        shouldDirty: true,
      })
      setBindingModalOpen(false)
    },
    [activeModelName, activeTargetIndex, setFieldValue, targets]
  )

  const handleRunAllNow = async () => {
    try {
      setRunningAll(true)
      const res = await api.post('/api/degradation/run')
      if (res.data?.success) {
        toast.success(
          res.data?.message || t('Degradation check triggered successfully')
        )
      } else {
        toast.error(res.data?.message || t('Failed to trigger check'))
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err)
      toast.error(msg || t('Failed to trigger check'))
    } finally {
      setRunningAll(false)
    }
  }

  const handleRunTargetNow = async (channelId: number, modelName: string) => {
    const key = `${channelId}-${modelName}`
    try {
      setTestingModelKey(key)
      const res = await api.post('/api/degradation/run-target', {
        channel_id: channelId,
        model_name: modelName,
      })
      if (res.data?.success) {
        toast.success(
          res.data?.message || t('Target probe queued successfully')
        )
      } else {
        toast.error(res.data?.message || t('Failed to test target'))
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err)
      toast.error(msg || t('Failed to test target'))
    } finally {
      setTestingModelKey(null)
    }
  }

  const onSubmit = async (values: DegradationCheckFormValues) => {
    if (configInvalid) {
      toast.error(
        t(
          'Probe or target configuration is not valid JSON; fix it before saving'
        )
      )
      return
    }
    const baseline = form.formState.defaultValues?.degradation_setting
    const hasJsonDraft = (['probes', 'targets'] as const).some(
      (field) => values.degradation_setting[field] !== baseline?.[field]
    )
    if (
      hasJsonDraft &&
      queryClient.getQueryState(['system-options'])?.status === 'error'
    ) {
      try {
        await queryClient.invalidateQueries(
          { queryKey: ['system-options'] },
          { throwOnError: true }
        )
      } catch {
        toast.error(t('Failed to load'))
        return
      }
    }
    const latestOptions = getOptionValue(
      queryClient.getQueryData<SystemOptionsResponse>(['system-options'])?.data,
      props.defaultValues
    )
    for (const field of ['probes', 'targets'] as const) {
      const submitted = values.degradation_setting[field]
      const latest = latestOptions[`degradation_setting.${field}`]
      if (
        submitted !== baseline?.[field] &&
        latest !== baseline?.[field] &&
        latest !== submitted
      ) {
        toast.error(
          t(
            'Probe or target configuration changed elsewhere. Reset to reload before saving.'
          )
        )
        return
      }
    }
    const savedDefaults = {
      degradation_setting: { ...baseline },
    }
    try {
      // Options persist one key at a time: stop at the first failure so later
      // keys stay untouched; keys saved before the failure remain applied.
      for (const [name, value] of Object.entries(values.degradation_setting)) {
        const field =
          name as keyof DegradationCheckFormValues['degradation_setting']
        // A refreshed server value must not be overwritten by an unedited draft.
        if (value === baseline?.[field]) continue
        const key = `degradation_setting.${name}`
        const res = await updateOption.mutateAsync({ key, value })
        if (res && !res.success) {
          throw new Error(res.message || String(key))
        }
        Object.assign(savedDefaults.degradation_setting, { [field]: value })
      }
      // Finish a fresh refetch (cancelling older Query reads) before rebasing;
      // HTTP responses from cancelled queries must not replace the baseline.
      await queryClient.invalidateQueries({ queryKey: ['system-options'] })
      toast.success(t('Settings updated successfully'))
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err)
      // A partial write may still have an older read pending; finish a fresh
      // snapshot before acknowledging writes as the retry baseline.
      await queryClient.invalidateQueries({ queryKey: ['system-options'] })
      // 逐 key 保存意味着失败时前面已提交的 key 不会回滚，必须明示，
      // 避免管理员误以为整套配置都回到了保存前的状态。
      toast.error(
        `${msg || t('Failed to update settings')} — ${t('Some options may have already been applied')}`
      )
    } finally {
      const currentValues = form.getValues()
      const refreshedOptions = getOptionValue(
        queryClient.getQueryData<SystemOptionsResponse>(['system-options'])
          ?.data,
        props.defaultValues
      )
      for (const [name, value] of Object.entries(values.degradation_setting)) {
        const field =
          name as keyof DegradationCheckFormValues['degradation_setting']
        if (value !== baseline?.[field]) continue
        // A new draft still derives from the original baseline, including
        // JSON conflicts that a concurrent server refresh must not hide.
        if (currentValues.degradation_setting[field] !== value) continue
        const latest = refreshedOptions[`degradation_setting.${field}`]
        Object.assign(savedDefaults.degradation_setting, { [field]: latest })
        Object.assign(currentValues.degradation_setting, { [field]: latest })
      }
      // Only acknowledged writes become clean; failed and newer edits stay dirty.
      form.reset(savedDefaults, { keepValues: true })
      form.reset(currentValues, { keepDefaultValues: true })
    }
  }

  return (
    <Form {...form}>
      <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
        {/* 全局调度核心卡片 */}
        <SettingsSection title={t('Degradation Check Service')}>
          <SettingsControlGroup>
            <FormField
              control={form.control}
              name='degradation_setting.enabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>
                      {t('Enable Scheduled Degradation Check')}
                    </FormLabel>
                    <FormDescription>
                      {t(
                        'Asynchronously probe active channels and models on a recurring schedule.'
                      )}
                    </FormDescription>
                    <FormDescription>
                      {t(
                        'To publish the status page, also enable "Degradation Check" under Maintenance → Header Navigation.'
                      )}
                    </FormDescription>
                    <FormDescription>
                      {t(
                        'Each probe execution writes an audited consumption log; balance the interval and targets against logs-table growth.'
                      )}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />

            <div className='flex justify-end pt-2 pb-1'>
              <Button
                type='button'
                size='sm'
                variant='secondary'
                disabled={runningAll}
                onClick={handleRunAllNow}
              >
                <Play className='mr-1.5 h-3.5 w-3.5' />
                {runningAll ? t('Triggering...') : t('Trigger Full Check Now')}
              </Button>
            </div>

            <div className='grid grid-cols-1 gap-4 pt-2 md:grid-cols-3'>
              <FormField
                control={form.control}
                name='degradation_setting.interval_minutes'
                render={({ field: numField }) => (
                  <FormItem>
                    <FormLabel>{t('Check Interval (Minutes)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        {...safeNumberFieldProps(numField)}
                      />
                    </FormControl>
                    <FormDescription>{t('Min 1, max 1440')}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='degradation_setting.concurrency'
                render={({ field: numField }) => (
                  <FormItem>
                    <FormLabel>{t('Max Cross-Channel Concurrency')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        {...safeNumberFieldProps(numField)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t('Parallel channel workers (1..32)')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='degradation_setting.channel_jitter_ms'
                render={({ field: numField }) => (
                  <FormItem>
                    <FormLabel>{t('In-Channel Jitter Delay (ms)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        {...safeNumberFieldProps(numField)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t('Serial micro-delay to prevent 429')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='degradation_setting.retention_days'
                render={({ field: numField }) => (
                  <FormItem>
                    <FormLabel>{t('History Retention Days')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        {...safeNumberFieldProps(numField)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t('Days to keep historical logs (1..90)')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='degradation_setting.timeout_seconds'
                render={({ field: numField }) => (
                  <FormItem>
                    <FormLabel>{t('Hard Timeout (Seconds)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        {...safeNumberFieldProps(numField)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t('Single probe execution limit')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>

            <div className='border-border mt-4 border-t pt-4'>
              <FormField
                control={form.control}
                name='degradation_setting.public_mask_topology'
                render={({ field: maskField }) => (
                  <div className='flex items-center justify-between'>
                    <div className='space-y-0.5'>
                      <Label className='text-sm font-medium'>
                        {t('Mask Topology for Non-Admin Users')}
                      </Label>
                      <p className='text-muted-foreground text-xs'>
                        {t(
                          'Hide internal channel IDs and actual channel names from standard users on status board.'
                        )}
                      </p>
                    </div>
                    <Switch
                      checked={maskField.value}
                      onCheckedChange={maskField.onChange}
                    />
                  </div>
                )}
              />
            </div>
          </SettingsControlGroup>
        </SettingsSection>

        {/* 自定义探针规则库 */}
        <SettingsSection title={t('Custom Probes Library')}>
          {!parsedProbes.valid && (
            <div className='border-destructive/30 bg-destructive/10 text-destructive mb-4 rounded-lg border px-3.5 py-2 text-xs'>
              {t(
                'Stored probe configuration is not valid JSON; saving is disabled to prevent data loss.'
              )}
            </div>
          )}
          <div className='mb-4 flex items-center justify-between'>
            <span className='text-muted-foreground text-xs'>
              {t('Total Probes: {{count}}', { count: probes.length })}
            </span>
            <Button
              type='button'
              size='sm'
              variant='outline'
              onClick={() => {
                setEditingProbe(null)
                setProbeModalOpen(true)
              }}
            >
              <Plus className='mr-1 h-3.5 w-3.5' />
              {t('Add Custom Probe')}
            </Button>
          </div>

          {probes.length === 0 ? (
            <div className='text-muted-foreground rounded-xl border border-dashed p-6 text-center text-xs'>
              {t(
                'No custom probes configured yet. Click button above to create one.'
              )}
            </div>
          ) : (
            <div className='grid grid-cols-1 gap-3 md:grid-cols-2'>
              {probes.map((probe) => (
                <div
                  key={probe.id}
                  className='bg-card/50 space-y-2 rounded-xl border p-3.5 text-xs'
                >
                  <div className='flex items-center justify-between'>
                    <span className='text-foreground font-semibold'>
                      {probe.name}
                    </span>
                    <div className='flex items-center gap-1.5'>
                      <Button
                        type='button'
                        size='icon-sm'
                        variant='ghost'
                        aria-label={t('Edit Probe')}
                        onClick={() => {
                          setEditingProbe(probe)
                          setProbeModalOpen(true)
                        }}
                      >
                        <Edit3 className='h-3.5 w-3.5' />
                      </Button>
                      <Button
                        type='button'
                        size='icon-sm'
                        variant='ghost'
                        onClick={() => handleDeleteProbeClick(probe)}
                      >
                        <Trash2 className='text-destructive h-3.5 w-3.5' />
                      </Button>
                    </div>
                  </div>
                  <p className='text-muted-foreground line-clamp-2 font-mono'>
                    "{probe.prompt}"
                  </p>
                  <div className='text-muted-foreground flex items-center justify-between border-t pt-2 text-[11px]'>
                    <span>
                      {t('Keywords: {{count}}', {
                        count: probe.keywords?.length || 0,
                      })}
                    </span>
                    <span>MaxTokens: {probe.max_tokens || 256}</span>
                  </div>
                </div>
              ))}
            </div>
          )}
        </SettingsSection>

        {/* 目标渠道与模型启停配置 */}
        <SettingsSection title={t('Monitored Targets & Models')}>
          {!parsedTargets.valid && (
            <div className='border-destructive/30 bg-destructive/10 text-destructive mb-4 rounded-lg border px-3.5 py-2 text-xs'>
              {t(
                'Stored target configuration is not valid JSON; saving is disabled to prevent data loss.'
              )}
            </div>
          )}
          <div className='mb-4 flex items-center justify-between'>
            <span className='text-muted-foreground text-xs'>
              {t('Total Monitored Channels: {{count}}', {
                count: targets.length,
              })}
            </span>
            <Button
              type='button'
              size='sm'
              variant='outline'
              onClick={() => setTargetModalOpen(true)}
            >
              <Plus className='mr-1 h-3.5 w-3.5' />
              {t('Add Target Channel')}
            </Button>
          </div>

          {targets.length === 0 ? (
            <div className='text-muted-foreground rounded-xl border border-dashed p-6 text-center text-xs'>
              {t('No detection targets configured.')}
            </div>
          ) : (
            <div className='space-y-3'>
              {targets.map((target, tIdx) => {
                const isAllModels = !target.models?.length
                const channelQuery =
                  channelQueries[
                    allModelTargets.findIndex(
                      (item) => item.channel_id === target.channel_id
                    )
                  ]
                const modelNames = isAllModels
                  ? [
                      ...new Set(
                        (channelQuery?.data?.data?.models || '')
                          .split(',')
                          .map((name) => name.trim())
                          .filter(Boolean)
                      ),
                    ]
                  : target.models || []
                return (
                  <div
                    key={target.channel_id}
                    className='bg-card/60 space-y-3 rounded-xl border p-4'
                  >
                    <div className='flex items-center justify-between text-xs'>
                      <div className='flex items-center gap-2'>
                        <span className='text-foreground font-semibold'>
                          {target.channel_name ||
                            t('Channel #{{id}}', { id: target.channel_id })}
                        </span>
                        <span className='bg-muted text-muted-foreground rounded px-1.5 py-0.5 font-mono text-[10px]'>
                          ID: {target.channel_id}
                        </span>
                      </div>

                      <Button
                        type='button'
                        size='icon-sm'
                        variant='ghost'
                        onClick={() => handleDeleteTarget(tIdx)}
                      >
                        <Trash2 className='text-destructive h-3.5 w-3.5' />
                      </Button>
                    </div>

                    <div className='space-y-2 border-t pt-2 text-xs'>
                      {isAllModels ? (
                        <div className='bg-muted/30 rounded-lg border border-dashed p-3'>
                          <div className='text-foreground font-mono font-medium'>
                            {t('All Models')}
                          </div>
                          {channelQuery?.isPending ? (
                            <p>{t('Loading current models...')}</p>
                          ) : null}
                          {channelQuery?.isError ? (
                            <div
                              role='alert'
                              className='mt-1 flex items-center gap-2'
                            >
                              <span>{t('Failed to load')}</span>
                              <Button
                                type='button'
                                variant='ghost'
                                size='sm'
                                onClick={() => void channelQuery.refetch()}
                              >
                                {t('Retry')}
                              </Button>
                            </div>
                          ) : null}
                          {channelQuery?.isSuccess &&
                          modelNames.length === 0 ? (
                            <p>{t('No models available')}</p>
                          ) : null}
                        </div>
                      ) : null}
                      {modelNames.map((modelName) => {
                        const binding = target.bindings?.find(
                          (b) => b.model_name === modelName
                        )
                        const isEnabled = binding?.enabled !== false
                        const reasoningEffort =
                          binding?.reasoning_effort || 'off'
                        const isTesting =
                          testingModelKey ===
                          `${target.channel_id}-${modelName}`

                        return (
                          <div
                            key={modelName}
                            role='group'
                            aria-label={modelName}
                            className={`flex flex-col justify-between gap-2 rounded-lg border p-2.5 transition sm:flex-row sm:items-center ${
                              isEnabled
                                ? 'bg-background'
                                : 'bg-muted/40 border-dashed opacity-60'
                            }`}
                          >
                            <div className='flex items-center gap-3'>
                              <Switch
                                aria-label={modelName}
                                checked={isEnabled}
                                onCheckedChange={(checked) =>
                                  handleToggleModelEnabled(
                                    tIdx,
                                    modelName,
                                    checked
                                  )
                                }
                              />
                              <div className='flex items-center gap-2'>
                                <span
                                  className={`font-mono font-medium ${
                                    !isEnabled
                                      ? 'text-muted-foreground line-through'
                                      : ''
                                  }`}
                                >
                                  {modelName}
                                </span>
                                {reasoningEffort !== 'off' && (
                                  <span className='rounded-full bg-purple-500/10 px-1.5 py-0.5 font-mono text-[10px] text-purple-600 dark:text-purple-400'>
                                    🧠 {reasoningEffort}
                                  </span>
                                )}
                                <span className='text-muted-foreground text-[11px]'>
                                  {isEnabled ? t('Enabled') : t('Paused')}
                                </span>
                              </div>
                            </div>

                            <div className='flex items-center gap-2'>
                              <Button
                                type='button'
                                size='sm'
                                variant='ghost'
                                className='h-7 text-xs'
                                disabled={isTesting}
                                onClick={() =>
                                  handleRunTargetNow(
                                    target.channel_id,
                                    modelName
                                  )
                                }
                              >
                                <Play className='text-muted-foreground mr-1 h-3 w-3' />
                                {isTesting ? t('Testing...') : t('Test Now')}
                              </Button>

                              <Button
                                type='button'
                                size='sm'
                                variant='ghost'
                                className='h-7 text-xs'
                                onClick={() =>
                                  handleOpenBindingDialog(tIdx, modelName)
                                }
                              >
                                <Brain className='mr-1 h-3.5 w-3.5' />
                                {t('Configure Probes & Reasoning')}
                              </Button>
                            </div>
                          </div>
                        )
                      })}
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </SettingsSection>

        <SettingsPageFormActions
          onSave={form.handleSubmit(onSubmit)}
          onReset={() => form.reset(defaultValues)}
          isSaving={isSubmitting}
          isSaveDisabled={configInvalid}
          isResetDisabled={!isDirty}
        />
      </SettingsForm>

      {/* 探针编辑/创建弹窗 */}
      <Dialog open={probeModalOpen} onOpenChange={setProbeModalOpen}>
        <DialogContent className='max-w-lg'>
          <DialogHeader>
            <DialogTitle>
              {editingProbe ? t('Edit Probe') : t('Create Probe')}
            </DialogTitle>
          </DialogHeader>
          <ProbeEditForm
            initialValue={editingProbe}
            onSave={handleSaveProbe}
            onCancel={() => setProbeModalOpen(false)}
          />
        </DialogContent>
      </Dialog>

      {/* 删除探针确认弹窗：列出显式引用该探针的绑定 */}
      <ConfirmDialog
        open={probePendingDelete !== null}
        onOpenChange={(open) => {
          if (!open) {
            setProbePendingDelete(null)
          }
        }}
        title={t('Delete Probe')}
        destructive
        confirmText={t('Delete')}
        handleConfirm={handleConfirmDeleteProbe}
        desc={
          probePendingDelete && probePendingDelete.affected.length > 0 ? (
            <div className='space-y-2 text-xs'>
              <p>
                {t(
                  'The following model bindings explicitly reference this probe and will stop running it after deletion:'
                )}
              </p>
              <ul className='max-h-40 space-y-1 overflow-y-auto font-mono'>
                {probePendingDelete.affected.map((item) => (
                  <li key={item}>{item}</li>
                ))}
              </ul>
            </div>
          ) : (
            t('No model bindings explicitly reference this probe.')
          )
        }
      />

      {/* 添加监测渠道弹窗 */}
      <AddTargetDialog
        open={targetModalOpen}
        onOpenChange={setTargetModalOpen}
        existingChannelIds={targets.map((t) => t.channel_id)}
        onAddTarget={handleAddTarget}
      />

      {/* 模型探针与思考配置弹窗 */}
      <ModelBindingDialog
        key={bindingSession}
        open={bindingModalOpen}
        onOpenChange={setBindingModalOpen}
        modelName={activeModelName}
        initialBinding={
          activeTargetIndex >= 0
            ? (targets[activeTargetIndex]?.bindings?.find(
                (b) => b.model_name === activeModelName
              ) ?? null)
            : null
        }
        probes={probes}
        onSave={handleSaveBinding}
      />
    </Form>
  )
}

function ProbeEditForm(props: {
  initialValue: ProbeConfig | null
  onSave: (probe: ProbeConfig) => void
  onCancel: () => void
}) {
  const { t } = useTranslation()
  const formId = useId()
  const [name, setName] = useState(props.initialValue?.name || '')
  const [prompt, setPrompt] = useState(props.initialValue?.prompt || '')
  const [systemPrompt, setSystemPrompt] = useState(
    props.initialValue?.system_prompt || ''
  )
  const [keywords, setKeywords] = useState(
    (props.initialValue?.keywords || []).join(
      props.initialValue?.match_rule === 'regex' ? '\n' : ', '
    )
  )
  const [excludeWords, setExcludeWords] = useState(
    (props.initialValue?.exclude_words || []).join(', ')
  )
  const [matchRule, setMatchRule] = useState<'any' | 'all' | 'regex'>(
    props.initialValue?.match_rule || 'any'
  )
  const [maxTokens, setMaxTokens] = useState<number>(
    props.initialValue?.max_tokens || 256
  )
  const [caseSensitive, setCaseSensitive] = useState<boolean>(
    props.initialValue?.case_sensitive || false
  )
  const [temperature, setTemperature] = useState<string>(
    props.initialValue?.temperature !== undefined
      ? String(props.initialValue.temperature)
      : ''
  )
  const matchRuleOptions = [
    { value: 'any', label: t('Any Match') },
    { value: 'all', label: t('All Match') },
    { value: 'regex', label: t('Regex') },
  ]

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!name.trim() || !prompt.trim()) {
      toast.error(t('Name and Prompt are required'))
      return
    }
    if (maxTokens < 1 || maxTokens > 1024) {
      toast.error(t('Max Tokens must be between 1 and 1024'))
      return
    }
    const kwList = keywords
      .split(matchRule === 'regex' ? /\r?\n/ : ',')
      .map((k) => k.trim())
      .filter(Boolean)
    const exList = excludeWords
      .split(',')
      .map((k) => k.trim())
      .filter(Boolean)
    const tempValue = temperature.trim()
    const tempNum = tempValue === '' ? undefined : Number(tempValue)
    if (
      tempNum !== undefined &&
      (Number.isNaN(tempNum) || tempNum < 0 || tempNum > 2)
    ) {
      toast.error(t('Temperature must be between 0 and 2'))
      return
    }

    const probe: ProbeConfig = {
      // 随机 id 避免同毫秒创建碰撞；无 secure context 时退回时间戳+随机串
      id:
        props.initialValue?.id ||
        (typeof globalThis.crypto?.randomUUID === 'function'
          ? globalThis.crypto.randomUUID()
          : `probe_${Date.now()}_${Math.random().toString(36).slice(2)}`),
      name: name.trim(),
      system_prompt: systemPrompt.trim() || undefined,
      prompt: prompt.trim(),
      keywords: kwList,
      exclude_words: exList.length > 0 ? exList : undefined,
      match_rule: matchRule,
      case_sensitive: caseSensitive,
      max_tokens: maxTokens,
      temperature: tempNum,
    }
    props.onSave(probe)
  }

  return (
    <form onSubmit={handleSubmit} className='space-y-3.5 text-xs'>
      <div>
        <Label htmlFor={`${formId}-name`}>{t('Probe Name')}</Label>
        <Input
          id={`${formId}-name`}
          value={name}
          onChange={(e) => setName(e.target.value)}
          className='mt-1 text-xs'
          placeholder={t('e.g. Basic Math Reasoning')}
          required
        />
      </div>

      <div>
        <Label htmlFor={`${formId}-system-prompt`}>
          {t('System Prompt (Optional)')}
        </Label>
        <Input
          id={`${formId}-system-prompt`}
          value={systemPrompt}
          onChange={(e) => setSystemPrompt(e.target.value)}
          className='mt-1 text-xs'
        />
      </div>

      <div>
        <Label htmlFor={`${formId}-prompt`}>{t('Test Prompt')}</Label>
        <Textarea
          id={`${formId}-prompt`}
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          className='mt-1 text-xs'
          rows={3}
          required
        />
      </div>

      <div className='grid grid-cols-2 gap-3'>
        <div>
          <Label htmlFor={`${formId}-keywords`}>
            {matchRule === 'regex'
              ? t('Expected Regular Expressions (one per line)')
              : t('Expected Keywords (comma-separated)')}
          </Label>
          {matchRule === 'regex' ? (
            <>
              <Textarea
                id={`${formId}-keywords`}
                value={keywords}
                onChange={(e) => setKeywords(e.target.value)}
                className='mt-1 text-xs'
                rows={3}
                placeholder={'^a,b$\n^c,d$'}
              />
              <p className='text-muted-foreground mt-1 text-[11px]'>
                {t(
                  'Enter one regular expression per line. Commas are preserved.'
                )}
              </p>
            </>
          ) : (
            <Input
              id={`${formId}-keywords`}
              value={keywords}
              onChange={(e) => setKeywords(e.target.value)}
              className='mt-1 text-xs'
              placeholder={t('wordA, wordB')}
            />
          )}
        </div>
        <div>
          <Label htmlFor={`${formId}-exclude-words`}>
            {t('Exclude Words (comma-separated)')}
          </Label>
          <Input
            id={`${formId}-exclude-words`}
            value={excludeWords}
            onChange={(e) => setExcludeWords(e.target.value)}
            className='mt-1 text-xs'
            placeholder={t('sorry, cannot')}
          />
        </div>
      </div>

      <div className='grid grid-cols-2 gap-3'>
        <div>
          <Label htmlFor={`${formId}-match-rule`}>{t('Match Rule')}</Label>
          <Select
            items={matchRuleOptions}
            value={matchRule}
            onValueChange={(val) =>
              val && setMatchRule(val as 'any' | 'all' | 'regex')
            }
          >
            <SelectTrigger
              id={`${formId}-match-rule`}
              className='mt-1 h-8 text-xs'
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {matchRuleOptions.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div>
          <Label htmlFor={`${formId}-max-tokens`}>{t('Max Tokens')}</Label>
          <Input
            id={`${formId}-max-tokens`}
            type='number'
            value={maxTokens}
            onChange={(e) =>
              setMaxTokens(Math.max(1, Math.floor(Number(e.target.value) || 1)))
            }
            className='mt-1 text-xs'
            min={1}
            max={1024}
          />
        </div>
      </div>

      <div className='grid grid-cols-2 items-center gap-3'>
        <div>
          <Label htmlFor={`${formId}-temperature`}>{t('Temperature')}</Label>
          <Input
            id={`${formId}-temperature`}
            type='number'
            value={temperature}
            onChange={(e) => setTemperature(e.target.value)}
            className='mt-1 text-xs'
            step='0.1'
            min={0}
            max={2}
            placeholder='0'
          />
        </div>
        <div className='flex items-center gap-2 pt-4'>
          <Checkbox
            id={`${formId}-case-sensitive`}
            checked={caseSensitive}
            onCheckedChange={(c) => setCaseSensitive(c === true)}
          />
          <Label htmlFor={`${formId}-case-sensitive`}>
            {t('Case Sensitive')}
          </Label>
        </div>
      </div>

      <DialogFooter className='pt-3'>
        <Button type='button' variant='outline' onClick={props.onCancel}>
          {t('Cancel')}
        </Button>
        <Button type='submit'>{t('Save Probe')}</Button>
      </DialogFooter>
    </form>
  )
}
