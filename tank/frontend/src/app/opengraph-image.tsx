import { ImageResponse } from "next/og";

export const alt = "Tank — Verifiable file storage";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";
export const dynamic = "force-static";

export default function Image() {
  return new ImageResponse(
    (
      <div
        style={{
          width: "100%",
          height: "100%",
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          justifyContent: "center",
          position: "relative",
          overflow: "hidden",
          background: "#07090b",
          color: "#f8f5ff",
        }}
      >
        <div
          style={{
            position: "absolute",
            top: 120,
            left: 110,
            width: 980,
            height: 720,
            borderRadius: "50%",
            border: "2px solid #67e8f9",
            background: "linear-gradient(180deg, #7135ae, #190c2b 45%, #07090b)",
          }}
        />
        <div style={{ display: "flex", fontSize: 18, color: "#d5c3f6", letterSpacing: 4 }}>
          VERIFIABLE FILE STORAGE
        </div>
        <div style={{ display: "flex", fontSize: 144, letterSpacing: 16, marginTop: 20 }}>
          TANK
        </div>
        <div style={{ display: "flex", fontSize: 28, marginTop: 16 }}>
          Tank it. Retrieve it. Verify it.
        </div>
      </div>
    ),
    size,
  );
}
