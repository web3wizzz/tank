"use client";

import { useEffect, useRef, useState } from "react";
import { ArrowUpRight, Menu, X } from "lucide-react";
import Logo from "@/components/logo";

const links = [
  { href: "#how-it-works", label: "How it works" },
  { href: "#technology", label: "Technology" },
  { href: "#roadmap", label: "Roadmap" },
  { href: "#resources", label: "Resources" },
];

export default function Navbar() {
  const [open, setOpen] = useState(false);
  const menuButton = useRef<HTMLButtonElement>(null);
  const firstLink = useRef<HTMLAnchorElement>(null);

  useEffect(() => {
    if (!open) return;
    firstLink.current?.focus();
    const close = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setOpen(false);
        requestAnimationFrame(() => menuButton.current?.focus());
      }
    };
    window.addEventListener("keydown", close);
    return () => window.removeEventListener("keydown", close);
  }, [open]);

  const closeMenu = () => setOpen(false);

  return (
    <header className="site-header">
      <div className="header-inner">
        <Logo />
        <nav className="desktop-nav" aria-label="Main navigation">
          {links.map((link) => (
            <a key={link.href} href={link.href}>{link.label}</a>
          ))}
        </nav>
        <a className="button primary nav-cta" href="#workspace">
          Open workspace <ArrowUpRight aria-hidden="true" size={15} />
        </a>
        <button
          ref={menuButton}
          className="menu-button"
          type="button"
          aria-expanded={open}
          aria-controls="mobile-navigation"
          aria-label={open ? "Close navigation menu" : "Open navigation menu"}
          onClick={() => setOpen((value) => !value)}
        >
          {open ? <X aria-hidden="true" /> : <Menu aria-hidden="true" />}
        </button>
      </div>
      <nav
        className={`mobile-nav ${open ? "mobile-nav-open" : ""}`}
        id="mobile-navigation"
        aria-label="Mobile navigation"
        aria-hidden={!open}
      >
        {links.map((link, index) => (
          <a
            key={link.href}
            ref={index === 0 ? firstLink : undefined}
            href={link.href}
            tabIndex={open ? 0 : -1}
            onClick={closeMenu}
          >
            {link.label}
          </a>
        ))}
        <a className="button primary" href="#workspace" tabIndex={open ? 0 : -1} onClick={closeMenu}>
          Open workspace <ArrowUpRight aria-hidden="true" size={15} />
        </a>
      </nav>
    </header>
  );
}
