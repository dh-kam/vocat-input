import React from 'react';
import { Sparkles, Plus, Cpu } from 'lucide-react';

export default function Navbar({ onOpenNewRun, backendOnline }) {
  return (
    <nav className="glass-card" style={{ borderRadius: 0, borderTop: 0, borderLeft: 0, borderRight: 0, padding: '1rem 2rem' }}>
      <div style={{ maxWidth: '1280px', margin: '0 auto', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
          <div style={{ 
            width: '40px', 
            height: '40px', 
            borderRadius: '12px', 
            background: 'var(--accent-gradient)', 
            display: 'flex', 
            alignItems: 'center', 
            justifyContent: 'center',
            boxShadow: 'var(--accent-glow)'
          }}>
            <Sparkles size={22} color="white" />
          </div>
          <div>
            <h1 style={{ fontSize: '1.25rem', fontWeight: 700, letterSpacing: '-0.02em', background: 'linear-gradient(to right, #ffffff, #94a3b8)', WebkitBackgroundClip: 'text', WebkitTextFillColor: 'transparent' }}>
              Vocat OCR Auto Pipeline
            </h1>
            <p style={{ fontSize: '0.75rem', color: 'var(--text-secondary)' }}>
              AI Multi-Engine OCR &bull; Standard JSON &bull; Auto DOC Generator
            </p>
          </div>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: '1rem' }}>
          <div className="glass-card" style={{ padding: '0.4rem 0.8rem', display: 'flex', alignItems: 'center', gap: '0.5rem', borderRadius: 'var(--radius-full)' }}>
            <Cpu size={16} color={backendOnline ? '#34d399' : '#f87171'} />
            <span style={{ fontSize: '0.8rem', fontWeight: 500, color: 'var(--text-secondary)' }}>
              {backendOnline ? 'Go API Backend Online' : 'Connecting Backend...'}
            </span>
          </div>

          <button className="btn-gradient" onClick={onOpenNewRun}>
            <Plus size={18} />
            <span>New Conversion Run</span>
          </button>
        </div>
      </div>
    </nav>
  );
}
