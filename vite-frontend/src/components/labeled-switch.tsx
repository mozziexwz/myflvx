import { Switch } from "@/shadcn-bridge/heroui/switch";

interface LabeledSwitchProps {
  label: React.ReactNode;
  description?: React.ReactNode;
  isSelected: boolean;
  isDisabled?: boolean;
  onValueChange: (value: boolean) => void;
}

// 带文字说明的开关（bridge 里的 Switch 不渲染 children）
export function LabeledSwitch({
  label,
  description,
  isSelected,
  isDisabled,
  onValueChange,
}: LabeledSwitchProps) {
  return (
    <div className="flex items-center justify-between gap-4 rounded-lg border border-divider px-3 py-2">
      <div className="min-w-0">
        <div className="text-sm text-foreground">{label}</div>
        {description ? (
          <div className="text-xs text-default-500 mt-0.5">{description}</div>
        ) : null}
      </div>
      <Switch
        isDisabled={isDisabled}
        isSelected={isSelected}
        onValueChange={onValueChange}
      />
    </div>
  );
}
