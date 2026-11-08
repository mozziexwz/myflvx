import { cn } from "@/lib/utils";

interface SelectableCardProps {
  selected: boolean;
  onToggle: () => void;
  className?: string;
  disabled?: boolean;
  children: React.ReactNode;
}

// 可点击切换选中的卡片。用 div 而不是 button，因为里面的 Radix Checkbox 本身就是 button。
export function SelectableCard({
  selected,
  onToggle,
  className,
  disabled = false,
  children,
}: SelectableCardProps) {
  return (
    <div
      aria-disabled={disabled}
      aria-pressed={selected}
      className={cn(
        "flex gap-2 rounded-lg border px-3 py-2 text-left select-none",
        disabled ? "cursor-not-allowed opacity-60" : "cursor-pointer",
        selected
          ? "border-primary-300 bg-primary-50/50 dark:border-primary-700 dark:bg-primary-900/20"
          : "border-divider",
        className,
      )}
      role="button"
      tabIndex={disabled ? -1 : 0}
      onClick={() => {
        if (!disabled) onToggle();
      }}
      onKeyDown={(e) => {
        if (disabled) return;
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onToggle();
        }
      }}
    >
      {children}
    </div>
  );
}
