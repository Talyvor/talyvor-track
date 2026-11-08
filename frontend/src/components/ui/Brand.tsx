import logoUrl from "~/assets/talyvor-logo-small-on-dark.svg";
import iconUrl from "~/assets/talyvor-favicon-tiny.svg";

// The brand files are copied from the logo kit (design/logo/talyvor-logo/svg), never redrawn: the
// wordmark is drawn, so TALYVOR is never set as live text. Track is dark only, so the on-dark files.

// TrackBrand is the logo with the product named beside it as an eyebrow — the board's product-UI
// sidebar header. 120 px wide, the smallest the kit allows the logo.
export function TrackBrand() {
  return (
    <div className="flex items-center gap-2.5">
      <img src={logoUrl} alt="Talyvor" width={120} height={38} className="h-[38px] w-[120px]" />
      <span className="text-[12px] font-medium uppercase tracking-[0.22em] text-label">Track</span>
    </div>
  );
}

// BrandMark is the app icon alone, for a header that already names its page: below 48 px the kit
// draws the icon, not the symbol.
export function BrandMark({ className = "h-7 w-7" }: { className?: string }) {
  return <img src={iconUrl} alt="Talyvor" className={className} />;
}
