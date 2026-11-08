import type { ReactNode } from "react";

interface PageIntroProps {
  title: string;
  description: ReactNode;
  className?: string;
}

// 页面左上角的标题和功能说明（myflvx 新增页面统一使用）
export function PageIntro({ title, description, className }: PageIntroProps) {
  return (
    <div className={className ?? "mb-4"}>
      <h1 className="text-lg font-semibold text-foreground">{title}</h1>
      <p className="mt-1 max-w-3xl text-sm leading-relaxed text-default-500">
        {description}
      </p>
    </div>
  );
}
