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
export type ShareLabelMeasurement = {
  anchor: number
  width: number
}

export type ShareLabelPlacement = {
  left: number
  lane: number
}

type LabelInterval = {
  left: number
  right: number
}

export function placeShareLabels(
  containerWidth: number,
  labels: ShareLabelMeasurement[],
  minGap = 8
): ShareLabelPlacement[] {
  const safeContainerWidth =
    Number.isFinite(containerWidth) && containerWidth > 0 ? containerWidth : 0
  const safeGap = Number.isFinite(minGap) && minGap > 0 ? minGap : 0
  const placements = labels.map<ShareLabelPlacement>(() => ({
    left: 0,
    lane: 0,
  }))
  const lanes: LabelInterval[][] = []

  const ordered = labels
    .map((label, index) => ({ label, index }))
    .sort((a, b) => {
      const anchorA = Number.isFinite(a.label.anchor) ? a.label.anchor : 0
      const anchorB = Number.isFinite(b.label.anchor) ? b.label.anchor : 0
      return anchorA - anchorB || a.index - b.index
    })

  for (const item of ordered) {
    const anchor = Math.min(
      1,
      Math.max(0, Number.isFinite(item.label.anchor) ? item.label.anchor : 0)
    )
    const width = Math.min(
      safeContainerWidth,
      Math.max(0, Number.isFinite(item.label.width) ? item.label.width : 0)
    )
    const unclampedLeft = Math.min(
      safeContainerWidth - width,
      Math.max(0, anchor * safeContainerWidth - width / 2)
    )
    const left = Math.round(unclampedLeft * 1000) / 1000
    const interval = { left, right: left + width }

    let lane = 0
    while (
      lanes[lane]?.some(
        (placed) =>
          interval.left < placed.right + safeGap &&
          interval.right + safeGap > placed.left
      )
    ) {
      lane += 1
    }

    if (!lanes[lane]) lanes[lane] = []
    lanes[lane].push(interval)
    placements[item.index] = { left, lane }
  }

  return placements
}
