import React, { useState, useEffect } from 'react';
import * as Toast from '@radix-ui/react-toast';
import { Sparkles, CheckCircle2, AlertCircle, Layers, ChevronDown } from 'lucide-react';
import RunList from './components/RunList';
import RunDetail from './components/RunDetail';
import NewRunModal from './components/NewRunModal';

const API_BASE = import.meta.env.VITE_API_BASE ?? '/api';

export default function App({ embedded = false, apiBase } = {}) {
  const resolvedApiBase = apiBase ?? API_BASE;
  const [runs, setRuns] = useState([]);
  const [selectedRunId, setSelectedRunId] = useState(null);
  const [selectedRun, setSelectedRun] = useState(null);
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [isLoading, setIsLoading] = useState(false);
  const [notification, setNotification] = useState(null);
  const [themeMode, setThemeMode] = useState(() => localStorage.getItem('vocat_theme') || 'nordic');
  const [isThemeOpen, setIsThemeOpen] = useState(false);

  useEffect(() => {
    if (embedded) return; // embedded: scope theme to the vocat-shell wrapper, don't touch host <html>/<body>
    document.documentElement.setAttribute('data-theme', themeMode);
    document.body.setAttribute('data-theme', themeMode);
  }, [themeMode, embedded]);

  const handleThemeChange = (newTheme) => {
    setThemeMode(newTheme);
    localStorage.setItem('vocat_theme', newTheme);
    showNotify(`🎨 UI Theme Mode switched to '${newTheme.toUpperCase()}'!`, 'success');
  };

  const showNotify = (msg, type = 'info') => {
    setNotification({ msg, type });
  };

  const ensureAuth = async () => {
    try {
      await fetch(`${resolvedApiBase}/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ secret: 'vocat_secure_session_secret_2026' })
      });
    } catch (err) {
      console.error('Session init error:', err);
    }
  };

  const fetchRuns = async () => {
    try {
      await ensureAuth();
      const res = await fetch(`${resolvedApiBase}/runs`, { credentials: 'include' });
      if (res.ok) {
        const data = await res.json();
        const runList = Array.isArray(data) ? data : (data.runs || []);
        // Sort by createdAt descending (newest first)
        runList.sort((a, b) => new Date(b.createdAt) - new Date(a.createdAt));
        setRuns(runList);

        setSelectedRunId((prev) => {
          if (prev && runList.some((r) => r.id === prev)) {
            return prev;
          }
          return runList.length > 0 ? runList[0].id : null;
        });
      }
    } catch (err) {
      console.error('Failed to fetch runs:', err);
    } finally {
      setIsLoading(false);
    }
  };

  const fetchRunDetail = async (id) => {
    if (!id) return;
    try {
      const res = await fetch(`${resolvedApiBase}/runs/${id}`, { credentials: 'include' });
      if (res.ok) {
        const data = await res.json();
        setSelectedRun(data || null);
      }
    } catch (err) {
      console.error('Failed to fetch run detail:', err);
    }
  };

  useEffect(() => {
    fetchRuns();
  }, []);

  useEffect(() => {
    // Clear previous run data immediately to prevent stale display
    setSelectedRun(null);
    if (selectedRunId) {
      fetchRunDetail(selectedRunId);
    }
  }, [selectedRunId]);

  // Live polling only when conversion is actively in progress
  useEffect(() => {
    if (!selectedRunId || !selectedRun) return;
    const isProcessing = 
      selectedRun.status === 'OCR_IN_PROGRESS' ||
      selectedRun.status === 'MERGING_CONVERTING' ||
      (selectedRun.progress > 0 && selectedRun.progress < 100);
    if (!isProcessing) return;
    const interval = setInterval(() => {
      fetchRunDetail(selectedRunId);
      fetchRuns();
    }, 350);
    return () => clearInterval(interval);
  }, [selectedRunId, selectedRun?.status, selectedRun?.progress]);

  const handleCreateRun = async (formData) => {
    try {
      showNotify('Uploading images & creating workflow...', 'info');
      const res = await fetch(`${resolvedApiBase}/runs`, {
        method: 'POST',
        credentials: 'include',
        body: formData,
      });
      if (res.ok) {
        const newRun = await res.json();
        showNotify('✨ New Vocat Run initialized!', 'success');
        setIsModalOpen(false);
        await fetchRuns();
        if (newRun && newRun.id) {
          setSelectedRunId(newRun.id);
          setSelectedRun(newRun);
        }
      } else {
        showNotify('Failed to create run', 'error');
      }
    } catch (err) {
      showNotify(`Error: ${err.message}`, 'error');
    }
  };

  const handleOneClickConvert = async (runId) => {
    try {
      showNotify('🚀 Launching Full AI Pipeline (OCR + Structuring + JSON + DOC)...', 'info');
      const res = await fetch(`${resolvedApiBase}/runs/${runId}/convert`, { method: 'POST', credentials: 'include' });
      if (res.ok) {
        showNotify('⚡ One-Click Conversion pipeline running in background!', 'success');
        fetchRunDetail(runId);
        fetchRuns();
      } else {
        showNotify('Failed to start conversion', 'error');
      }
    } catch (err) {
      showNotify(`Error: ${err.message}`, 'error');
    }
  };

  const handleRegenerateDoc = async (runId) => {
    try {
      showNotify('🔄 Regenerating DOC Test Sheet from updated JSON data...', 'info');
      const res = await fetch(`${resolvedApiBase}/runs/${runId}/regenerate-doc`, { method: 'POST', credentials: 'include' });
      if (res.ok) {
        showNotify('🎉 DOC Test Sheet successfully regenerated from updated JSON!', 'success');
        fetchRunDetail(runId);
      } else {
        showNotify('Failed to regenerate DOC sheet', 'error');
      }
    } catch (err) {
      showNotify(`Error: ${err.message}`, 'error');
    }
  };

  const handleSendTelegram = async (runId) => {
    try {
      showNotify('✈️ Sending DOC Test Sheet to Telegram...', 'info');
      const res = await fetch(`${resolvedApiBase}/runs/${runId}/send-telegram`, { method: 'POST', credentials: 'include' });
      if (res.ok) {
        showNotify('🎉 Document successfully delivered to Telegram chat!', 'success');
        fetchRunDetail(runId);
      } else {
        const err = await res.json();
        showNotify(`Telegram Delivery Failed: ${err.error}`, 'error');
      }
    } catch (err) {
      showNotify(`Error: ${err.message}`, 'error');
    }
  };

  const handleUpdateWords = async (runId, words) => {
    try {
      const res = await fetch(`${resolvedApiBase}/runs/${runId}/words`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ words }),
      });
      if (res.ok) {
        showNotify('💾 Vocabulary words updated successfully!', 'success');
        fetchRunDetail(runId);
      } else {
        showNotify('Failed to update words', 'error');
      }
    } catch (err) {
      showNotify(`Error: ${err.message}`, 'error');
    }
  };

  const handleUpdateTitle = async (runId, newTitle) => {
    try {
      const res = await fetch(`${resolvedApiBase}/runs/${runId}/title`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ title: newTitle }),
      });
      if (res.ok) {
        const updated = await res.json();
        showNotify('✏️ Run title updated successfully!', 'success');
        if (selectedRun && selectedRun.id === runId) {
          setSelectedRun(prev => prev ? { ...prev, title: newTitle } : prev);
        }
        fetchRunDetail(runId);
        fetchRuns();
      } else {
        const errData = await res.json().catch(() => ({}));
        showNotify(`Failed to update title: ${errData.error || res.statusText}`, 'error');
      }
    } catch (err) {
      showNotify(`Error: ${err.message}`, 'error');
    }
  };

  const handleDeleteRun = async (runId) => {
    try {
      const res = await fetch(`${resolvedApiBase}/runs/${runId}`, {
        method: 'DELETE',
        credentials: 'include',
      });
      if (res.ok) {
        showNotify('🗑️ Run and associated files deleted!', 'success');
        if (selectedRunId === runId) {
          setSelectedRunId(null);
          setSelectedRun(null);
        }
        fetchRuns();
      } else {
        showNotify('Failed to delete run', 'error');
      }
    } catch (err) {
      showNotify(`Error: ${err.message}`, 'error');
    }
  };

  const handleTriggerADB = async (runId) => {
    try {
      showNotify('📱 Triggering ADB Vocat Android Auto-Input process...', 'info');
      const res = await fetch(`${resolvedApiBase}/runs/${runId}/adb-input`, { method: 'POST', credentials: 'include' });
      if (res.ok) {
        const data = await res.json();
        showNotify(`✅ ADB Auto-Input Triggered! Log: ${data.logPath}`, 'success');
      } else {
        showNotify('Failed to trigger ADB process', 'error');
      }
    } catch (err) {
      showNotify(`Error: ${err.message}`, 'error');
    }
  };

  return (
    <Toast.Provider swipeDirection="right">
      <div className={`min-h-screen flex flex-col font-sans${embedded ? ' vocat-shell' : ''}`} data-theme={themeMode} style={{ backgroundColor: 'var(--bg-canvas)', color: 'var(--text-main)', transition: 'background-color 0.35s ease, color 0.35s ease' }}>

        {/* Radix Light Header (hidden when embedded — the host provides the chrome) */}
        {!embedded && (
        <header className="sticky top-0 z-40 backdrop-blur-xl border-b shadow-sm px-8 py-5" style={{ backgroundColor: 'var(--panel-bg)', borderColor: 'var(--border-color)' }}>
          <div className="max-w-[1700px] mx-auto flex flex-wrap items-center justify-between gap-4">
            
            <div className="flex items-center gap-4">
              <div className="w-12 h-12 rounded-2xl bg-gradient-to-br from-sky-500 to-indigo-600 flex items-center justify-center text-white shadow-lg shadow-sky-500/25 ring-2 ring-white/20">
                <Sparkles className="w-6 h-6" />
              </div>
              <div>
                <h1 className="text-2xl font-extrabold tracking-tight">Vocat Input</h1>
                <p className="text-sm font-medium opacity-70 mt-0.5">Multi-Cloud AI Vision OCR & Bounding Box Evidence Studio</p>
              </div>
            </div>

            {/* Header Right: Collapsible Theme Picker */}
            <div className="relative">
              {(() => {
                const themes = [
                  { id: 'nordic', label: 'Nordic Light', emoji: '❄️', pill: 'bg-sky-600 text-white' },
                  { id: 'midnight', label: 'Midnight', emoji: '🌌', pill: 'bg-slate-800 text-cyan-300' },
                  { id: 'cyberpunk', label: 'Cyberpunk', emoji: '🔮', pill: 'bg-pink-600 text-white' },
                  { id: 'emerald', label: 'Emerald', emoji: '🌿', pill: 'bg-emerald-600 text-white' }
                ];
                const current = themes.find(t => t.id === themeMode) || themes[0];
                return (
                  <>
                    {/* Collapsed: just the current theme */}
                    <button
                      type="button"
                      onClick={() => setIsThemeOpen(!isThemeOpen)}
                      className={`flex items-center gap-2 px-4 py-2 rounded-xl text-sm font-bold shadow-sm transition-all duration-200 ${current.pill} hover:opacity-90`}
                    >
                      <span>{current.emoji} {current.label}</span>
                      <ChevronDown className={`w-4 h-4 transition-transform duration-200 ${isThemeOpen ? 'rotate-180' : ''}`} />
                    </button>

                    {/* Expanded: dropdown options */}
                    <div
                      className={`absolute right-0 top-full mt-2 z-50 rounded-2xl shadow-2xl border overflow-hidden transition-all duration-250 origin-top-right ${
                        isThemeOpen
                          ? 'opacity-100 scale-100 pointer-events-auto'
                          : 'opacity-0 scale-95 pointer-events-none'
                      }`}
                      style={{ backgroundColor: 'var(--panel-bg)', borderColor: 'var(--border-color)', minWidth: '180px' }}
                    >
                      <div className="p-1.5 space-y-0.5">
                        {themes.map(t => (
                          <button
                            key={t.id}
                            type="button"
                            onClick={() => {
                              handleThemeChange(t.id);
                              setIsThemeOpen(false);
                            }}
                            className={`w-full flex items-center gap-2.5 px-3.5 py-2.5 rounded-xl text-sm font-bold transition-all duration-150 ${
                              themeMode === t.id
                                ? `${t.pill} shadow-sm`
                                : 'hover:bg-slate-500/10 opacity-80 hover:opacity-100'
                            }`}
                          >
                            <span className="text-base">{t.emoji}</span>
                            <span>{t.label}</span>
                            {themeMode === t.id && <CheckCircle2 className="w-4 h-4 ml-auto" />}
                          </button>
                        ))}
                      </div>
                    </div>

                    {/* Backdrop to close */}
                    {isThemeOpen && (
                      <div
                        className="fixed inset-0 z-40"
                        onClick={() => setIsThemeOpen(false)}
                      />
                    )}
                  </>
                );
              })()}
            </div>

          </div>
        </header>
        )}

        {/* Radix UI Floating Toast Notification */}
        {notification && (
          <Toast.Root 
            open={Boolean(notification)} 
            onOpenChange={(open) => !open && setNotification(null)}
            className="fixed top-20 right-8 z-50 px-3.5 py-2 rounded-xl bg-white/95 backdrop-blur-md border border-slate-200/90 shadow-lg shadow-slate-900/10 flex items-center gap-2.5 border-l-4 border-l-sky-500 transition-all duration-200"
          >
            {notification.type === 'error' ? (
              <AlertCircle className="w-4 h-4 text-rose-500 shrink-0" />
            ) : (
              <CheckCircle2 className="w-4 h-4 text-sky-600 shrink-0" />
            )}
            <Toast.Title className="text-xs font-extrabold text-slate-800 tracking-tight">
              {notification.msg}
            </Toast.Title>
          </Toast.Root>
        )}
        <Toast.Viewport />

        {/* Main Grid Layout */}
        <main className="max-w-[1700px] w-full mx-auto p-8 flex-1 grid grid-cols-1 lg:grid-cols-[380px_1fr] gap-8">
          <aside className="h-full">
            <RunList
              runs={runs}
              selectedRunId={selectedRunId}
              onSelectRun={(id) => setSelectedRunId(id)}
              onNewRun={() => {
                setSelectedRunId(null);
                setSelectedRun(null);
                setIsModalOpen(true);
              }}
              onDeleteRun={handleDeleteRun}
            />
          </aside>

          <section className="h-full">
            <RunDetail
              run={selectedRun}
              onOneClickConvert={handleOneClickConvert}
              onRegenerateDoc={handleRegenerateDoc}
              onSendTelegram={handleSendTelegram}
              onUpdateWords={handleUpdateWords}
              onUpdateTitle={handleUpdateTitle}
              onTriggerADB={handleTriggerADB}
            />
          </section>
        </main>

        <NewRunModal
          isOpen={isModalOpen}
          onClose={() => setIsModalOpen(false)}
          onSubmit={handleCreateRun}
        />

      </div>
    </Toast.Provider>
  );
}
