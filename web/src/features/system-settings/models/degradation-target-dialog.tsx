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
import { useQuery } from '@tanstack/react-query'
import { Check, ChevronsUpDown, Loader2, Search } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
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
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { searchChannels } from '@/features/channels/api'
import { channelsQueryKeys } from '@/features/channels/lib/channel-actions'
import type { Channel } from '@/features/channels/types'

import type { DetectionTarget } from './degradation-check-section'

export type AddTargetDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  existingChannelIds: number[]
  onAddTarget: (target: DetectionTarget) => void
}

export function AddTargetDialog(props: AddTargetDialogProps) {
  const { t } = useTranslation()
  const [selectedChannel, setSelectedChannel] = useState<Channel | null>(null)
  const [selectedModels, setSelectedModels] = useState<string[]>([])
  const [comboboxOpen, setComboboxOpen] = useState(false)
  const [channelSearch, setChannelSearch] = useState('')
  const [modelFilter, setModelFilter] = useState('')

  const {
    data: channels = [],
    isLoading: isLoadingChannels,
    isError: isChannelsError,
    refetch: refetchChannels,
  } = useQuery({
    queryKey: channelsQueryKeys.list({ page_size: 1000 }),
    queryFn: async () => {
      const res = await searchChannels({ page_size: 1000 })
      if (!res.success) {
        throw new Error(res.message || 'Failed to fetch channels')
      }
      return res.data?.items ?? []
    },
    enabled: props.open,
    staleTime: 60_000,
  })

  const filteredChannels = useMemo(() => {
    const q = channelSearch.trim().toLowerCase()
    if (!q) return channels
    return channels.filter((ch) => {
      const idMatch = String(ch.id).includes(q)
      const nameMatch = ch.name?.toLowerCase().includes(q)
      const groupMatch = ch.group?.toLowerCase().includes(q)
      return idMatch || nameMatch || groupMatch
    })
  }, [channels, channelSearch])

  const availableModels = useMemo<string[]>(() => {
    if (!selectedChannel?.models) return []
    return [
      ...new Set(
        selectedChannel.models
          .split(',')
          .map((m) => m.trim())
          .filter(Boolean)
      ),
    ]
  }, [selectedChannel])

  const filteredModels = useMemo(() => {
    const q = modelFilter.trim().toLowerCase()
    if (!q) return availableModels
    return availableModels.filter((m) => m.toLowerCase().includes(q))
  }, [availableModels, modelFilter])

  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen) {
      setSelectedChannel(null)
      setSelectedModels([])
      setComboboxOpen(false)
      setChannelSearch('')
      setModelFilter('')
    }
    props.onOpenChange(nextOpen)
  }

  const handleSelectChannel = (channel: Channel) => {
    setSelectedChannel(channel)
    setSelectedModels([]) // 需求要求：仅列出模型供手动勾选，不默认全选
    setComboboxOpen(false)
    setChannelSearch('')
    setModelFilter('')
  }

  const handleToggleModel = (modelName: string) => {
    setSelectedModels((prev) =>
      prev.includes(modelName)
        ? prev.filter((m) => m !== modelName)
        : [...prev, modelName]
    )
  }

  const handleSelectAll = () => {
    setSelectedModels([...availableModels])
  }

  const handleClearAll = () => {
    setSelectedModels([])
  }

  const handleAdd = () => {
    if (!selectedChannel) {
      toast.error(t('Please select a channel'))
      return
    }
    if (props.existingChannelIds.includes(selectedChannel.id)) {
      toast.error(t('This channel is already being monitored'))
      return
    }
    if (selectedModels.length === 0) {
      toast.error(t('Please select at least one model to monitor'))
      return
    }

    const newTarget: DetectionTarget = {
      channel_id: selectedChannel.id,
      channel_name: selectedChannel.name?.trim() || undefined,
      models: selectedModels,
      bindings: selectedModels.map((m) => ({
        model_name: m,
        enabled: true,
      })),
    }

    props.onAddTarget(newTarget)
    handleOpenChange(false)
  }

  const channelPlaceholder = isLoadingChannels
    ? t('Loading channels...')
    : t('Select a channel...')

  const renderChannelListContent = () => {
    if (isLoadingChannels) {
      return (
        <div className='text-muted-foreground flex items-center justify-center gap-2 py-6 text-xs'>
          <Loader2 className='h-4 w-4 animate-spin' />
          <span>{t('Loading channels...')}</span>
        </div>
      )
    }
    if (isChannelsError) {
      return (
        <div className='p-4 text-center text-xs'>
          <p className='text-destructive'>{t('Failed to load')}</p>
          <Button
            type='button'
            variant='ghost'
            size='sm'
            className='mt-1 text-xs'
            onClick={() => void refetchChannels()}
          >
            {t('Retry')}
          </Button>
        </div>
      )
    }
    if (filteredChannels.length === 0) {
      return <CommandEmpty>{t('No channels found.')}</CommandEmpty>
    }
    return (
      <CommandGroup>
        {filteredChannels.map((channel) => {
          const isAlreadyAdded = props.existingChannelIds.includes(channel.id)
          const isSelected = selectedChannel?.id === channel.id
          return (
            <CommandItem
              key={channel.id}
              value={`${channel.id}-${channel.name}`}
              role='option'
              aria-selected={isSelected}
              disabled={isAlreadyAdded}
              onSelect={() => handleSelectChannel(channel)}
              className='flex items-center justify-between gap-2 py-2 text-xs'
            >
              <div className='flex min-w-0 items-center gap-2'>
                <span className='bg-muted text-muted-foreground rounded px-1.5 py-0.5 font-mono text-[10px]'>
                  #{channel.id}
                </span>
                <span className='truncate font-medium'>{channel.name}</span>
                {channel.group && (
                  <span className='text-muted-foreground truncate text-[10px]'>
                    ({channel.group})
                  </span>
                )}
              </div>
              <div className='flex shrink-0 items-center gap-2'>
                {isAlreadyAdded && (
                  <span className='bg-muted text-muted-foreground rounded px-1.5 py-0.5 text-[10px]'>
                    {t('Already added')}
                  </span>
                )}
                {isSelected && <Check className='text-primary h-3.5 w-3.5' />}
              </div>
            </CommandItem>
          )
        })}
      </CommandGroup>
    )
  }

  const renderModelsContent = () => {
    if (!selectedChannel) {
      return (
        <div className='text-muted-foreground rounded-lg border border-dashed p-4 text-center text-xs'>
          {t('Please select a channel to view its models.')}
        </div>
      )
    }
    if (availableModels.length === 0) {
      return (
        <div className='text-muted-foreground rounded-lg border border-dashed p-4 text-center text-xs'>
          {t('This channel has no models configured.')}
        </div>
      )
    }
    return (
      <div className='space-y-2'>
        {availableModels.length > 6 && (
          <div className='relative'>
            <Search className='text-muted-foreground absolute top-2.5 left-2.5 h-3.5 w-3.5' />
            <Input
              type='text'
              value={modelFilter}
              onChange={(e) => setModelFilter(e.target.value)}
              placeholder={t('Filter models...')}
              className='h-8 pl-8 text-xs'
            />
          </div>
        )}
        <div className='divide-border bg-card/40 max-h-52 divide-y overflow-y-auto rounded-lg border p-1'>
          {filteredModels.length === 0 ? (
            <div className='text-muted-foreground py-4 text-center text-xs'>
              {t('No matching models')}
            </div>
          ) : (
            filteredModels.map((modelName) => {
              const isChecked = selectedModels.includes(modelName)
              const checkboxId = `target-model-${modelName}`
              return (
                <div
                  key={modelName}
                  className='hover:bg-muted/50 flex items-center gap-2.5 rounded-md px-2.5 py-1.5 text-xs transition-colors select-none'
                >
                  <Checkbox
                    id={checkboxId}
                    checked={isChecked}
                    onCheckedChange={() => handleToggleModel(modelName)}
                  />
                  <Label
                    htmlFor={checkboxId}
                    className='text-foreground flex-1 cursor-pointer font-mono text-xs font-medium'
                  >
                    {modelName}
                  </Label>
                </div>
              )
            })
          )}
        </div>
      </div>
    )
  }

  return (
    <Dialog open={props.open} onOpenChange={handleOpenChange}>
      <DialogContent className='max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Add Target Channel')}</DialogTitle>
        </DialogHeader>

        <div className='space-y-4 py-2 text-xs'>
          {/* 渠道选择 */}
          <div className='space-y-1.5'>
            <Label>{t('Target Channel')}</Label>
            <Popover open={comboboxOpen} onOpenChange={setComboboxOpen}>
              <PopoverTrigger
                render={
                  <Button
                    type='button'
                    variant='outline'
                    role='combobox'
                    aria-expanded={comboboxOpen}
                    aria-label={t('Target Channel')}
                    className='h-9 w-full justify-between text-xs font-normal'
                  />
                }
              >
                {selectedChannel ? (
                  <div className='flex min-w-0 flex-1 items-center gap-2'>
                    <span className='bg-muted text-muted-foreground rounded px-1.5 py-0.5 font-mono text-[10px]'>
                      #{selectedChannel.id}
                    </span>
                    <span className='truncate font-medium'>
                      {selectedChannel.name ||
                        t('Channel #{{id}}', { id: selectedChannel.id })}
                    </span>
                    {selectedChannel.group && (
                      <span className='text-muted-foreground ml-auto truncate text-[10px]'>
                        ({selectedChannel.group})
                      </span>
                    )}
                  </div>
                ) : (
                  <span className='text-muted-foreground'>
                    {channelPlaceholder}
                  </span>
                )}
                <ChevronsUpDown className='text-muted-foreground ml-2 h-4 w-4 shrink-0 opacity-50' />
              </PopoverTrigger>

              <PopoverContent
                align='start'
                className='w-[var(--radix-popover-trigger-width)] min-w-[340px] p-0'
              >
                <Command shouldFilter={false}>
                  <CommandInput
                    value={channelSearch}
                    onValueChange={setChannelSearch}
                    placeholder={t('Search channel by name, ID or group...')}
                  />
                  <CommandList>{renderChannelListContent()}</CommandList>
                </Command>
              </PopoverContent>
            </Popover>
          </div>

          {/* 模型列表展示与选择 */}
          <div className='space-y-1.5'>
            <div className='flex items-center justify-between'>
              <Label>
                {t('Monitored Models')}
                {selectedChannel && (
                  <span className='text-muted-foreground ml-1 font-normal'>
                    (
                    {t('Selected {{count}} of {{total}}', {
                      count: selectedModels.length,
                      total: availableModels.length,
                    })}
                    )
                  </span>
                )}
              </Label>
              {selectedChannel && availableModels.length > 0 && (
                <div className='flex items-center gap-1'>
                  <Button
                    type='button'
                    variant='ghost'
                    size='sm'
                    className='h-6 px-1.5 text-[11px]'
                    onClick={handleSelectAll}
                  >
                    {t('Select all')}
                  </Button>
                  <span className='text-muted-foreground text-[10px]'>|</span>
                  <Button
                    type='button'
                    variant='ghost'
                    size='sm'
                    className='h-6 px-1.5 text-[11px]'
                    onClick={handleClearAll}
                  >
                    {t('Clear All')}
                  </Button>
                </div>
              )}
            </div>

            {renderModelsContent()}
          </div>
        </div>

        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            onClick={() => handleOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='button'
            onClick={handleAdd}
            disabled={!selectedChannel || selectedModels.length === 0}
          >
            {t('Add Target')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
