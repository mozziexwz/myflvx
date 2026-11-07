import { cn } from "@/lib/utils";

interface SelectableCardProps {
  selected: boolean;
  onToggle: () => void;
  className?: string;
  children: React.ReactNode;
}

// 可点击切换选中的卡片。用 div 而不是 button，因为里面的 Radix Checkbox 本身就是 button。
export function SelectableCard({
  selected,
  onToggle,
  className,
  children,
}: SelectableCardProps) {
  return (
    <div
      aria-pressed={selected}
      className={cn(
        "flex gap-2 rounded-lg border px-3 py-2 cursor-pointer text-left select-none",
        selected
          ? "border-primary-300 bg-primary-50/50 dark:border-primary-700 dark:bg-primary-900/20"
          : "border-divider",
        className,
      )}
      role="button"
      tabIndex={0}
      onClick={onToggle}
      onKeyDown={(e) => {
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
