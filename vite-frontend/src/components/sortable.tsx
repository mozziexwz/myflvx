// 拖拽排序的公共组件（与原版「规则」「节点」页面相同的 @dnd-kit 交互）
import type {
  DragEndEvent,
  DraggableAttributes,
  DraggableSyntheticListeners,
} from "@dnd-kit/core";
import type { CSSProperties, ReactNode } from "react";

import {
  KeyboardSensor,
  MouseSensor,
  TouchSensor,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import {
  arrayMove,
  sortableKeyboardCoordinates,
  useSortable,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";

import { cn } from "@/lib/utils";
import { TableRow } from "@/shadcn-bridge/heroui/table";

export interface SortableHandleProps {
  attributes: DraggableAttributes;
  listeners: DraggableSyntheticListeners;
}

// 鼠标拖动 8px 后开始，触屏长按 250ms，支持键盘
export function useSortSensors() {
  return useSensors(
    useSensor(MouseSensor, { activationConstraint: { distance: 8 } }),
    useSensor(TouchSensor, {
      activationConstraint: { delay: 250, tolerance: 8 },
    }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  );
}

export function DragHandle({
  handle,
  className,
}: {
  handle: SortableHandleProps;
  className?: string;
}) {
  return (
    <button
      aria-label="拖拽排序"
      className={cn(
        "inline-flex cursor-grab items-center justify-center rounded p-1 text-default-400 transition-colors hover:text-default-600 active:cursor-grabbing",
        className,
      )}
      style={{ touchAction: "none" }}
      title="拖拽排序"
      type="button"
      {...handle.attributes}
      {...handle.listeners}
    >
      <svg
        aria-hidden="true"
        className="h-4 w-4"
        fill="currentColor"
        viewBox="0 0 20 20"
      >
        <path d="M7 2a2 2 0 1 1 .001 4.001A2 2 0 0 1 7 2zm0 6a2 2 0 1 1 .001 4.001A2 2 0 0 1 7 8zm0 6a2 2 0 1 1 .001 4.001A2 2 0 0 1 7 14zm6-8a2 2 0 1 1-.001-4.001A2 2 0 0 1 13 6zm0 2a2 2 0 1 1 .001 4.001A2 2 0 0 1 13 8zm0 6a2 2 0 1 1 .001 4.001A2 2 0 0 1 13 14z" />
      </svg>
    </button>
  );
}

const useSortableStyle = (id: number) => {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id });
  const style: CSSProperties = {
    transform: CSS.Translate.toString(
      transform
        ? {
            ...transform,
            x: Math.round(transform.x),
            y: Math.round(transform.y),
          }
        : null,
    ),
    transition: isDragging ? undefined : transition,
    opacity: isDragging ? 0.6 : 1,
    position: isDragging ? "relative" : undefined,
    zIndex: isDragging ? 20 : undefined,
  };

  return { handle: { attributes, listeners }, setNodeRef, style };
};

export function SortableTableRow({
  id,
  className,
  children,
}: {
  id: number;
  className?: string;
  children: (handle: SortableHandleProps) => ReactNode;
}) {
  const { handle, setNodeRef, style } = useSortableStyle(id);

  return (
    <TableRow ref={setNodeRef} className={className} style={style}>
      {children(handle)}
    </TableRow>
  );
}

export function SortableCard({
  id,
  className,
  children,
}: {
  id: number;
  className?: string;
  children: (handle: SortableHandleProps) => ReactNode;
}) {
  const { handle, setNodeRef, style } = useSortableStyle(id);

  return (
    <div ref={setNodeRef} className={cn("h-full", className)} style={style}>
      {children(handle)}
    </div>
  );
}

// 拖拽结束时被拖动的 id 与目标 id；没有移动时返回 null
export function draggedIds(event: DragEndEvent): [number, number] | null {
  const { active, over } = event;

  if (!over || active.id === over.id) return null;
  const activeId = Number(active.id);
  const overId = Number(over.id);

  if (!Number.isFinite(activeId) || !Number.isFinite(overId)) return null;

  return [activeId, overId];
}

// 在当前显示的条目（可能经过筛选）之间移动，其余条目位置不变，返回完整的新顺序
export function reorderIds(
  order: number[],
  displayedIds: number[],
  activeId: number,
  overId: number,
): number[] | null {
  const oldIndex = displayedIds.indexOf(activeId);
  const newIndex = displayedIds.indexOf(overId);

  if (oldIndex < 0 || newIndex < 0 || oldIndex === newIndex) return null;
  const moved = arrayMove(displayedIds, oldIndex, newIndex);
  const displayed = new Set(displayedIds);
  let next = 0;

  return order.map((id) => {
    if (!displayed.has(id)) return id;
    const value = moved[next];

    next += 1;

    return value;
  });
}

// 按新顺序排列列表并更新 inx
export function applyOrder<T extends { id: number; inx: number }>(
  list: T[],
  order: number[],
): T[] {
  const byId = new Map(list.map((item) => [item.id, item]));

  return order
    .map((id, inx) => {
      const item = byId.get(id);

      return item ? { ...item, inx } : null;
    })
    .filter((item): item is T => item !== null);
}

export const orderItems = (order: number[]) =>
  order.map((id, inx) => ({ id, inx }));
