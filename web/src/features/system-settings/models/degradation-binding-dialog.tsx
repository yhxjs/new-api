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
import { Brain } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import type {
  ModelProbeBinding,
  ProbeConfig,
} from './degradation-check-section'

type ModelBindingDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  modelName: string
  initialBinding: ModelProbeBinding | null
  probes: ProbeConfig[]
  onSave: (binding: ModelProbeBinding) => void
}

// 父组件通过递增的 key 在每次打开时重挂载本组件，useState 的初始值因此
// 每次都会从 initialBinding 重新计算，无需额外的重置 effect。
export function ModelBindingDialog(props: ModelBindingDialogProps) {
  const { t } = useTranslation()
  // An omitted or empty list means all configured probes to the executor.
  // Keep that representation so adding a probe later does not require editing
  // every existing binding.
  const [selectedProbeIds, setSelectedProbeIds] = useState<string[] | null>(
    () => {
      const probeIds = props.initialBinding?.probe_ids
      if (!probeIds?.length) return null
      return probeIds.filter((id) =>
        props.probes.some((probe) => probe.id === id)
      )
    }
  )
  const [selectedReasoningEffort, setSelectedReasoningEffort] =
    useState<string>(props.initialBinding?.reasoning_effort || 'inherit')
  const [customThinkingBudget, setCustomThinkingBudget] = useState<number>(
    props.initialBinding?.thinking_budget || 2048
  )
  const reasoningOptions = [
    { value: 'inherit', label: t('Use model default') },
    { value: 'off', label: t('Off / No Reasoning') },
    { value: 'low', label: t('Low') },
    { value: 'medium', label: t('Medium') },
    { value: 'high', label: t('High') },
    { value: 'custom', label: t('Custom Budget') },
  ]

  const handleSave = () => {
    if (selectedProbeIds?.length === 0) {
      toast.error(t('Select at least one probe'))
      return
    }
    props.onSave({
      model_name: props.modelName,
      enabled: props.initialBinding?.enabled ?? true,
      probe_ids: selectedProbeIds ?? undefined,
      reasoning_effort:
        selectedReasoningEffort === 'inherit'
          ? undefined
          : selectedReasoningEffort,
      thinking_budget:
        selectedReasoningEffort === 'custom' ? customThinkingBudget : undefined,
    })
  }

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='max-w-md'>
        <DialogHeader>
          <DialogTitle>
            {t('Configure Model Probes: {{model}}', { model: props.modelName })}
          </DialogTitle>
        </DialogHeader>
        <div className='space-y-4 py-2 text-xs'>
          {/* 思考等级选择 */}
          <div className='space-y-2 rounded-xl border border-purple-500/20 bg-purple-500/5 p-3.5'>
            <Label className='flex items-center gap-1.5 font-medium'>
              <Brain className='h-3.5 w-3.5 text-purple-600' />
              {t('Reasoning Effort / Thinking Budget')}
            </Label>
            <Select
              items={reasoningOptions}
              value={selectedReasoningEffort}
              onValueChange={(val) => val && setSelectedReasoningEffort(val)}
            >
              <SelectTrigger className='h-8 w-full text-xs'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {reasoningOptions.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            {selectedReasoningEffort === 'custom' && (
              <div className='flex items-center justify-between pt-1'>
                <span className='text-muted-foreground'>
                  {t('Thinking Budget Tokens:')}
                </span>
                <Input
                  type='number'
                  className='h-7 w-28 text-xs'
                  value={customThinkingBudget}
                  min={1}
                  max={65536}
                  onChange={(e) => {
                    const parsed = Math.floor(Number(e.target.value))
                    setCustomThinkingBudget(
                      Number.isFinite(parsed) && parsed > 0
                        ? Math.min(parsed, 65536)
                        : 2048
                    )
                  }}
                />
              </div>
            )}
          </div>

          {/* 探针选择多选 */}
          <div className='space-y-2'>
            <Label className='font-medium'>{t('Select Active Probes')}</Label>
            {props.probes.length === 0 ? (
              <p className='text-muted-foreground text-[11px]'>
                {t('No probes available in library.')}
              </p>
            ) : (
              <div className='max-h-48 space-y-1.5 overflow-y-auto pr-1'>
                {props.probes.map((p) => {
                  const checked =
                    selectedProbeIds === null || selectedProbeIds.includes(p.id)
                  return (
                    <label
                      key={p.id}
                      className='hover:bg-muted/50 flex cursor-pointer items-center gap-2 rounded-lg border p-2'
                    >
                      <Checkbox
                        checked={checked}
                        onCheckedChange={(c) => {
                          if (c) {
                            if (selectedProbeIds === null) return
                            const next = [...selectedProbeIds, p.id]
                            setSelectedProbeIds(
                              props.probes.every((probe) =>
                                next.includes(probe.id)
                              )
                                ? null
                                : next
                            )
                          } else {
                            setSelectedProbeIds(
                              (
                                selectedProbeIds ??
                                props.probes.map((probe) => probe.id)
                              ).filter((id) => id !== p.id)
                            )
                          }
                        }}
                      />
                      <span className='text-foreground font-medium'>
                        {p.name}
                      </span>
                    </label>
                  )
                })}
              </div>
            )}
          </div>
        </div>
        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button type='button' onClick={handleSave}>
            {t('Save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
