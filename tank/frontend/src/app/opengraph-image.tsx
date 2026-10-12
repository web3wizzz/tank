import { ImageResponse } from "next/og";

export const alt = "Tank — encrypted, verifiable file storage";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";
export const dynamic = "force-static";

export default function Image() {
  return new ImageResponse(
    <div style={{ width: "100%", height: "100%", display: "flex", flexDirection: "column", justifyContent: "space-between", padding: "72px 82px", position: "relative", overflow: "hidden", background: "#0B0F19", color: "#fff", fontFamily: "sans-serif" }}>
      <div style={{ position: "absolute", right: -120, top: -180, width: 640, height: 640, borderRadius: "50%", background: "radial-gradient(circle, rgba(0,229,255,.17), rgba(11,15,25,0) 68%)" }} />
      <div style={{ display: "flex", alignItems: "center", gap: 20, fontSize: 38, fontWeight: 700 }}>
        <svg width="54" height="54" viewBox="0 0 40 40" fill="none"><path d="M6.5 10.25 20 3l13.5 7.25v19.5L20 37 6.5 29.75v-19.5Z" stroke="#fff" strokeWidth="1.7"/><path d="M12.5 13.25h15M20 13.25v17" stroke="#00E5FF" strokeWidth="3.2"/></svg>
        Tank
      </div>
      <div style={{ display: "flex", flexDirection: "column" }}>
        <div style={{ display: "flex", fontSize: 70, lineHeight: 1.03, letterSpacing: -3, fontWeight: 760 }}>TANK YOUR FILES.</div>
        <div style={{ display: "flex", fontSize: 70, lineHeight: 1.03, letterSpacing: -3, fontWeight: 760, color: "#00E5FF" }}>RETRIEVE WITH CONFIDENCE.</div>
      </div>
      <div style={{ display: "flex", justifyContent: "space-between", fontSize: 21, color: "#94A3B8" }}><span>Encrypted · Distributed · Verifiable</span><span>Open-source local MVP</span></div>
    </div>,
    size,
  );
}
