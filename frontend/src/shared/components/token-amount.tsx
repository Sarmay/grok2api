import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/shared/lib/cn";
import { formatCompactTokens, formatNumber } from "@/shared/lib/format";

export function TokenAmount({ value, locale, className }: { value: number; locale: string; className?: string }) {
  const compact = formatCompactTokens(value, locale);
  const exact = formatNumber(value, locale, 0);
  if (compact === exact) {
    return <span className={cn("tabular-nums", className)}>{compact}</span>;
  }
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className={cn("cursor-help tabular-nums", className)} tabIndex={0}>{compact}</span>
      </TooltipTrigger>
      <TooltipContent side="top"><span className="font-mono">{exact}</span></TooltipContent>
    </Tooltip>
  );
}
