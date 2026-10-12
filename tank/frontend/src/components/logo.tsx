import Link from "next/link";

type LogoProps = {
  compact?: boolean;
  monochrome?: boolean;
  className?: string;
};

export function LogoMark({ monochrome = false }: Pick<LogoProps, "monochrome">) {
  return (
    <svg
      aria-hidden="true"
      className="logo-mark"
      viewBox="0 0 40 40"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
    >
      <path
        d="M6.5 10.25 20 3l13.5 7.25v19.5L20 37 6.5 29.75v-19.5Z"
        stroke="currentColor"
        strokeWidth="1.7"
      />
      <path
        d="m6.5 10.25 13.5 7.3 13.5-7.3M20 17.55V37"
        stroke="currentColor"
        strokeWidth="1.25"
        opacity=".55"
      />
      <path
        d="M12.5 13.25h15M20 13.25v17"
        stroke={monochrome ? "currentColor" : "#00E5FF"}
        strokeWidth="3.2"
        strokeLinecap="square"
      />
    </svg>
  );
}

export default function Logo({ compact = false, monochrome = false, className = "" }: LogoProps) {
  return (
    <Link
      className={`brand ${compact ? "brand-compact" : ""} ${className}`}
      href="/"
      aria-label="Tank home"
    >
      <LogoMark monochrome={monochrome} />
      <span className="brand-word">Tank</span>
    </Link>
  );
}
