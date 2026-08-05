import React, { useState, useEffect, useRef } from 'react';
import * as Tabs from '@radix-ui/react-tabs';
import * as Dialog from '@radix-ui/react-dialog';
import { 
  Sparkles, FileJson, FileText, Smartphone, CheckCircle2, Loader2, 
  Edit3, Plus, Trash2, Save, Eye, Terminal, Send, Image as ImageIcon, 
  Layers, RefreshCw, ArrowUp, ArrowDown, Search, X, Pencil, Check, AlertTriangle
} from 'lucide-react';
import { CroppedEvidenceThumbnail, InteractiveRedBoxModal } from './BoundingBoxViewer';
import DocPreviewModal from './DocPreviewModal';

export default function RunDetail({ 
  run, 
  onOneClickConvert, 
  onRegenerateDoc, 
  onSendTelegram, 
  onUpdateWords,
  onUpdateTitle,
  onTriggerADB 
}) {
  const [editableWords, setEditableWords] = useState([]);
  const [searchQuery, setSearchQuery] = useState('');
  const [isEditing, setIsEditing] = useState(false);
  const [isSavingWords, setIsSavingWords] = useState(false);
  const [selectedWord, setSelectedWord] = useState(null);
  
  // Title editing state
  const [isEditingTitle, setIsEditingTitle] = useState(false);
  const [titleInput, setTitleInput] = useState('');

  // Discard changes modal state
  const [showDiscardModal, setShowDiscardModal] = useState(false);
  
  // Doc preview state
  const [showDocPreview, setShowDocPreview] = useState(false);
  const [docPreviewData, setDocPreviewData] = useState(null);

  const logContainerRef = useRef(null);
  const isAtBottomRef = useRef(true);

  useEffect(() => {
    if (run) {
      if (Array.isArray(run.words) && run.words.length > 0) {
        const sorted = [...run.words].sort((a, b) => (a.no || 0) - (b.no || 0));
        setEditableWords(sorted);
      } else {
        setEditableWords([]);
      }
      setTitleInput(run.title || run.id);
    } else {
      setEditableWords([]);
      setTitleInput('');
    }
  }, [run?.id, run?.words]);

  // ESC Key listener for Edit Mode
  useEffect(() => {
    if (!isEditing) return;

    const handleKeyDown = (e) => {
      if (e.key === 'Escape') {
        const originalJson = JSON.stringify(run?.words || []);
        const currentJson = JSON.stringify(editableWords);
        if (originalJson !== currentJson) {
          setShowDiscardModal(true);
        } else {
          setIsEditing(false);
        }
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isEditing, editableWords, run?.words]);

  // Smart Auto-Scroll when new logs arrive (only if user is focused at the bottom)
  useEffect(() => {
    if (logContainerRef.current && isAtBottomRef.current) {
      logContainerRef.current.scrollTop = logContainerRef.current.scrollHeight;
    }
  }, [run?.logs]);

  const handleLogScroll = () => {
    if (!logContainerRef.current) return;
    const { scrollTop, scrollHeight, clientHeight } = logContainerRef.current;
    // Consider focused at bottom if within 40px of bottom
    isAtBottomRef.current = scrollHeight - scrollTop - clientHeight < 40;
  };

  const scrollToTop = () => {
    if (logContainerRef.current) {
      logContainerRef.current.scrollTop = 0;
      isAtBottomRef.current = false;
    }
  };

  const scrollToBottom = () => {
    if (logContainerRef.current) {
      logContainerRef.current.scrollTop = logContainerRef.current.scrollHeight;
      isAtBottomRef.current = true;
    }
  };

  if (!run) {
    return (
      <div className="h-full flex flex-col items-center justify-center p-12 border border-slate-200 rounded-3xl bg-white/80 text-center shadow-sm">
        <div className="w-16 h-16 mb-4 rounded-3xl bg-sky-50 border border-sky-200 flex items-center justify-center text-sky-600">
          <Layers className="w-8 h-8" />
        </div>
        <h3 className="text-lg font-bold text-slate-800">No Run Selected</h3>
        <p className="text-sm text-slate-500 mt-1 max-w-sm">Select a workflow run from the left panel or create a new run to view AI OCR & vocabulary extraction evidence</p>
      </div>
    );
  }

  const filteredWords = (editableWords || []).filter((w) => {
    if (!searchQuery.trim()) return true;
    const q = searchQuery.toLowerCase().trim();
    const wordMatch = w.word && w.word.toLowerCase().includes(q);
    const meaningMatch = typeof w.meaning === 'string' && w.meaning.toLowerCase().includes(q);
    const posMatch = w.pos && w.pos.toLowerCase().includes(q);
    const noMatch = String(w.no || '').includes(q);
    return wordMatch || meaningMatch || posMatch || noMatch;
  });

  const guessPosFromMeaning = (meaning) => {
    if (!meaning || typeof meaning !== 'string') return null;
    const trimmed = meaning.trim();
    if (!trimmed) return null;

    // Adjective endings (~한, ~는, ~은, ~스러운, ~적, ~된, ~인, ~갈, ~운)
    if (/(?:한|는|은|스러운|적|된|인|갈|운)$/.test(trimmed)) {
      return '형';
    }
    // Verb endings (~하다, ~되다, ~시키다, ~있다, ~없다, ~가다, ~오다, ~나다)
    if (/(?:하다|되다|시키다|있다|없다|가다|오다|나다)$/.test(trimmed)) {
      return '동';
    }
    // Adverb endings (~하게, ~히)
    if (/(?:하게|히)$/.test(trimmed)) {
      return '부';
    }
    return null;
  };

  const handleWordChange = (index, field, value) => {
    const updated = [...editableWords];
    let pos = updated[index].pos || '명';

    if (field === 'meaning') {
      const guessed = guessPosFromMeaning(value);
      if (guessed) {
        pos = guessed;
      }
    }

    updated[index] = { ...updated[index], [field]: value, pos };
    setEditableWords(updated);
  };

  const handleSaveTitle = () => {
    if (titleInput.trim() && onUpdateTitle) {
      onUpdateTitle(run.id, titleInput.trim());
    }
    setIsEditingTitle(false);
  };

  const handleAddWord = () => {
    const nextNo = editableWords.length + 1;
    setEditableWords([...editableWords, { no: nextNo, word: '', pos: '명', meaning: '' }]);
  };

  const handleDeleteWord = (index) => {
    setEditableWords(editableWords.filter((_, i) => i !== index));
  };

  const handleSaveWords = async () => {
    setIsSavingWords(true);
    await onUpdateWords(run.id, editableWords);
    if (onRegenerateDoc) {
      await onRegenerateDoc(run.id);
    }
    setIsSavingWords(false);
    setIsEditing(false);
  };

  const handleDownloadDoc = async () => {
    try {
      const res = await fetch(`http://localhost:8080/api/runs/${run.id}/download/doc`, {
        credentials: 'include',
      });
      if (!res.ok) throw new Error('DOC download failed');
      const blob = await res.blob();
      const url = window.URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.style.display = 'none';
      a.href = url;
      a.download = `${run.title || run.id}.doc`;
      document.body.appendChild(a);
      a.click();
      window.URL.revokeObjectURL(url);
      document.body.removeChild(a);
    } catch (err) {
      console.error('Error downloading DOC:', err);
    }
  };

  const handlePreviewDoc = async () => {
    try {
      const res = await fetch(`http://localhost:8080/api/runs/${run.id}/download/doc`, {
        credentials: 'include',
      });
      if (!res.ok) throw new Error('DOC preview failed');
      const json = await res.json();
      setDocPreviewData(json);
      setShowDocPreview(true);
    } catch (err) {
      console.error('Error previewing DOC:', err);
      alert('Could not preview DOC file. It might not be generated yet or is in invalid format.');
    }
  };

  const handleDownloadJSON = async () => {
    try {
      const res = await fetch(`http://localhost:8080/api/runs/${run.id}/download/json`, {
        credentials: 'include',
      });
      if (!res.ok) throw new Error('JSON download failed');
      const blob = await res.blob();
      const url = window.URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.style.display = 'none';
      a.href = url;
      a.download = `${run.title || run.id}.json`;
      document.body.appendChild(a);
      a.click();
      window.URL.revokeObjectURL(url);
      document.body.removeChild(a);
    } catch (err) {
      console.error('Error downloading JSON:', err);
    }
  };

  const getSourceImageForWord = (word) => {
    if (!run || !Array.isArray(run.ocrResults) || run.ocrResults.length === 0) return null;
    const idx = ((word && word.imageIndex) || 1) - 1;
    if (idx >= 0 && idx < run.ocrResults.length) {
      return run.ocrResults[idx];
    }
    return run.ocrResults[0];
  };

  const isDone = run.status === 'COMPLETED';
  const isInProg = run.progress > 0 && run.progress < 100;

  return (
    <div className="flex flex-col space-y-6">
      
      {/* Top Banner & Control Bar */}
      <div className="p-7 rounded-3xl border shadow-md space-y-6" style={{ backgroundColor: 'var(--card-bg)', borderColor: 'var(--border-color)' }}>
        
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex items-center gap-4">
            <div className="w-14 h-14 rounded-2xl bg-sky-50 border border-sky-200 flex items-center justify-center text-sky-600 shadow-inner">
              <Sparkles className="w-7 h-7" />
            </div>
            <div>
              <div className="flex items-center gap-3">
                {isEditingTitle ? (
                  <div className="flex items-center gap-2">
                    <input
                      type="text"
                      value={titleInput}
                      onChange={(e) => setTitleInput(e.target.value)}
                      onKeyDown={(e) => e.key === 'Enter' && handleSaveTitle()}
                      autoFocus
                      className="bg-white border border-sky-400 rounded-xl px-3 py-1 text-lg font-black text-slate-900 focus:outline-none focus:ring-2 focus:ring-sky-500/20 shadow-sm"
                    />
                    <button
                      type="button"
                      onClick={handleSaveTitle}
                      className="p-1.5 rounded-xl bg-emerald-600 hover:bg-emerald-700 text-white shadow-sm transition-all"
                      title="Save title"
                    >
                      <Check className="w-4 h-4" />
                    </button>
                    <button
                      type="button"
                      onClick={() => setIsEditingTitle(false)}
                      className="p-1.5 rounded-xl bg-slate-200 hover:bg-slate-300 text-slate-700 transition-all"
                      title="Cancel"
                    >
                      <X className="w-4 h-4" />
                    </button>
                  </div>
                ) : (
                  <div className="flex items-center gap-2 group cursor-pointer" onClick={() => setIsEditingTitle(true)}>
                    <h1 className="text-2xl font-black text-slate-900 tracking-tight hover:text-sky-600 transition-colors">
                      {run.title || run.id}
                    </h1>
                    <button
                      type="button"
                      className="p-1 text-slate-400 hover:text-sky-600 rounded-lg hover:bg-slate-100 transition-all"
                      title="Edit run title"
                    >
                      <Pencil className="w-4 h-4" />
                    </button>
                  </div>
                )}

                <span className="px-3 py-1 rounded-full text-xs font-extrabold uppercase tracking-wider bg-sky-100 text-sky-800 border border-sky-200">
                  {run.ocrProvider} {run.ocrModel ? `(${run.ocrModel})` : ''}
                </span>
              </div>
              <p className="text-sm font-medium text-slate-500 mt-1">Created at {new Date(run.createdAt).toLocaleString()}</p>
            </div>
          </div>

          {/* Action Buttons */}
          <div className="flex items-center gap-3 flex-wrap">
            {(run.status === 'CREATED' || run.status === 'FAILED') && (
              <button 
                onClick={() => onOneClickConvert(run.id)}
                className={`px-6 py-3 rounded-xl text-sm font-extrabold flex items-center gap-2 shadow-lg transition-all ${
                  run.status === 'FAILED'
                    ? 'bg-rose-600 hover:bg-rose-700 text-white shadow-rose-500/25'
                    : 'bg-gradient-to-r from-sky-600 to-indigo-600 hover:from-sky-700 hover:to-indigo-700 text-white shadow-sky-500/25'
                }`}
              >
                <RefreshCw className="w-5 h-5" /> 
                {run.status === 'FAILED' ? 'Retry Conversion (OCR + AI)' : 'Convert (OCR + AI Structuring)'}
              </button>
            )}

            {(run.status === 'OCR_COMPLETED' || isDone) && (
              <button 
                onClick={() => onRegenerateDoc(run.id)}
                className="px-5 py-2.5 rounded-xl text-sm font-bold bg-pink-50 border border-pink-200 text-pink-700 hover:bg-pink-100 transition-all flex items-center gap-2"
              >
                <RefreshCw className="w-4.5 h-4.5 text-pink-600" /> Regenerate DOC
              </button>
            )}

            {isDone && (
              <>
                <button 
                  onClick={() => onSendTelegram(run.id)}
                  className="px-5 py-2.5 rounded-xl text-sm font-bold bg-sky-50 border border-sky-200 text-sky-700 hover:bg-sky-100 transition-all flex items-center gap-2"
                >
                  <Send className="w-4.5 h-4.5 text-sky-600" /> Send to Telegram
                </button>

                <button 
                  type="button"
                  onClick={handleDownloadDoc}
                  className="px-4 py-2.5 rounded-xl text-sm font-bold bg-white hover:bg-slate-50 border border-slate-300 text-slate-700 shadow-sm flex items-center gap-2 transition-all cursor-pointer"
                >
                  <FileText className="w-4.5 h-4.5 text-sky-600" /> .DOC Sheet
                </button>

                <button 
                  type="button"
                  onClick={handlePreviewDoc}
                  className="px-4 py-2.5 rounded-xl text-sm font-bold bg-white hover:bg-slate-50 border border-slate-300 text-slate-700 shadow-sm flex items-center gap-2 transition-all cursor-pointer"
                >
                  <Eye className="w-4.5 h-4.5 text-indigo-600" /> Preview DOC
                </button>

                <button 
                  type="button"
                  onClick={handleDownloadJSON}
                  className="px-4 py-2.5 rounded-xl text-sm font-bold bg-white hover:bg-slate-50 border border-slate-300 text-slate-700 shadow-sm flex items-center gap-2 transition-all cursor-pointer"
                >
                  <FileJson className="w-4.5 h-4.5 text-purple-600" /> .JSON Data
                </button>

                <button 
                  onClick={() => onTriggerADB(run.id)}
                  className="px-4 py-2.5 rounded-xl text-sm font-bold bg-emerald-50 hover:bg-emerald-100 border border-emerald-200 text-emerald-800 transition-colors flex items-center gap-2"
                >
                  <Smartphone className="w-4.5 h-4.5 text-emerald-600" /> ADB Auto-Input
                </button>
              </>
            )}
          </div>
        </div>

        {/* Summary Banner */}
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 p-4 rounded-2xl bg-slate-50 border border-slate-200">
          <div className="flex items-center gap-3 px-3 py-1 border-r border-slate-200">
            <div className="w-9 h-9 rounded-xl bg-sky-100 border border-sky-200 flex items-center justify-center text-sky-700">
              <ImageIcon className="w-4.5 h-4.5" />
            </div>
            <div>
              <span className="text-xs font-bold uppercase text-slate-500 tracking-wider">Total Source Images</span>
              <p className="text-base font-black text-slate-900">{run.images?.length || 0} Images</p>
            </div>
          </div>

          <div className="flex items-center gap-3 px-3 py-1 border-r border-slate-200">
            <div className={`w-9 h-9 rounded-xl flex items-center justify-center ${
              isInProg && !run.words?.length
                ? 'bg-amber-100 border border-amber-200 text-amber-600'
                : 'bg-purple-100 border border-purple-200 text-purple-700'
            }`}>
              {isInProg && !run.words?.length ? (
                <Loader2 className="w-4.5 h-4.5 animate-spin" />
              ) : (
                <Sparkles className="w-4.5 h-4.5" />
              )}
            </div>
            <div>
              <span className="text-xs font-bold uppercase text-slate-500 tracking-wider">Extracted Vocabulary</span>
              {isInProg && !run.words?.length ? (
                <p className="text-sm font-bold text-amber-700 flex items-center gap-1.5">
                  추출 중<span className="inline-flex"><span className="animate-pulse">.</span><span className="animate-pulse" style={{animationDelay:'0.2s'}}>.</span><span className="animate-pulse" style={{animationDelay:'0.4s'}}>.</span></span>
                </p>
              ) : (
                <p className="text-base font-black text-purple-800">{run.words?.length || 0} Words</p>
              )}
            </div>
          </div>

          <div className="flex items-center gap-3 px-3 py-1">
            <div className={`w-9 h-9 rounded-xl flex items-center justify-center ${
              isInProg && !run.words?.length
                ? 'bg-sky-100 border border-sky-200 text-sky-600'
                : 'bg-emerald-100 border border-emerald-200 text-emerald-700'
            }`}>
              {isInProg && !run.words?.length ? (
                <Loader2 className="w-4.5 h-4.5 animate-spin" />
              ) : (
                <CheckCircle2 className="w-4.5 h-4.5" />
              )}
            </div>
            <div>
              <span className="text-xs font-bold uppercase text-slate-500 tracking-wider">
                {isInProg && !run.words?.length ? 'Processing Status' : 'Extraction Summary'}
              </span>
              {isInProg && !run.words?.length ? (
                <p className="text-sm font-bold text-sky-700">
                  {run.images?.length || 0}장 이미지 OCR 처리 중 ({run.progress || 0}%)
                </p>
              ) : (
                <p className="text-sm font-extrabold text-emerald-800">
                  {run.images?.length || 0}장 이미지에서 {run.words?.length || 0}개 단어 추출 완료
                </p>
              )}
            </div>
          </div>
        </div>

        {/* Dynamic Pulsing Progress Track Card */}
        <div className={`p-5 rounded-2xl border transition-all duration-300 ${
          isInProg 
            ? 'bg-sky-50/80 border-sky-300 shadow-md ring-2 ring-sky-500/20' 
            : 'bg-slate-50 border-slate-200'
        }`}>
          <div className="flex items-center justify-between mb-2.5">
            <div className="flex items-center gap-3">
              {isInProg ? (
                <div className="relative flex items-center justify-center">
                  <Loader2 className="w-7 h-7 text-sky-600 animate-spin" />
                  <Sparkles className="w-3.5 h-3.5 text-sky-500 absolute animate-pulse" />
                </div>
              ) : (
                <CheckCircle2 className="w-6 h-6 text-emerald-600" />
              )}
              <div>
                <span className="text-sm font-extrabold text-slate-900">
                  {isInProg ? '🚀 Conversion Pipeline Running...' : '🎉 Workflow Completed'}
                </span>
                <p className="text-xs font-bold text-sky-700 mt-0.5">
                  {run.logs && run.logs.length > 0 ? run.logs[run.logs.length - 1] : 'Pipeline ready'}
                </p>
              </div>
            </div>

            <div className="flex items-baseline gap-1">
              <span className="text-3xl font-black font-mono text-sky-700 tracking-tight">
                {run.progress || 0}
              </span>
              <span className="text-base font-black text-sky-700">%</span>
            </div>
          </div>

          <div className="w-full h-3 bg-slate-200 rounded-full overflow-hidden relative">
            <div 
              className="h-full bg-gradient-to-r from-sky-500 to-indigo-600 rounded-full transition-all duration-300"
              style={{ width: `${run.progress || 0}%` }}
            />
          </div>
        </div>

      </div>

      {/* Error Alert Banner */}
      {(run.status === 'FAILED' || run.error) && (
        <div className="p-5 bg-rose-50 border border-rose-200 rounded-2xl flex items-start justify-between gap-4 text-rose-800 shadow-sm">
          <div className="flex items-start gap-3.5">
            <div className="w-10 h-10 rounded-xl bg-rose-100 border border-rose-200 flex items-center justify-center text-rose-600 shrink-0">
              <AlertTriangle className="w-5 h-5" />
            </div>
            <div>
              <h4 className="text-sm font-black uppercase tracking-wider text-rose-900">Conversion Workflow Failed</h4>
              <p className="text-xs font-medium mt-1 text-rose-700 break-all">{run.error || 'An unexpected error occurred during OCR or AI structuring.'}</p>
            </div>
          </div>
          <button
            onClick={() => onOneClickConvert(run.id)}
            className="px-4 py-2.5 bg-rose-600 hover:bg-rose-700 text-white text-xs font-black rounded-xl shrink-0 transition-all flex items-center gap-1.5 shadow-md shadow-rose-500/20"
          >
            <RefreshCw className="w-4 h-4" /> Retry Conversion
          </button>
        </div>
      )}

      {/* Radix UI Tabs Primitive */}
      <Tabs.Root defaultValue="words" className="space-y-5">
        <Tabs.List className="flex items-center gap-2 p-1.5 bg-slate-200/60 rounded-3xl w-fit border border-slate-300/60 mb-4">
          <Tabs.Trigger 
            value="words"
            className="flex items-center gap-2 px-5 py-3 text-sm font-extrabold rounded-2xl transition-all data-[state=active]:bg-white data-[state=active]:text-sky-800 data-[state=active]:border data-[state=active]:border-slate-300 data-[state=active]:shadow-sm text-slate-600 hover:text-slate-900"
          >
            <FileJson className="w-4.5 h-4.5 text-sky-600" />
            <span>Vocabulary Table ({editableWords.length})</span>
          </Tabs.Trigger>

          <Tabs.Trigger 
            value="logs"
            className="flex items-center gap-2 px-5 py-3 text-sm font-extrabold rounded-2xl transition-all data-[state=active]:bg-white data-[state=active]:text-sky-800 data-[state=active]:border data-[state=active]:border-slate-300 data-[state=active]:shadow-sm text-slate-600 hover:text-slate-900"
          >
            <Terminal className="w-4.5 h-4.5 text-sky-600" />
            <span>Backend Execution Terminal</span>
          </Tabs.Trigger>
        </Tabs.List>

        {/* Tab 1: Vocabulary Table */}
        <Tabs.Content value="words" className="p-7 rounded-3xl border shadow-md space-y-5 focus:outline-none" style={{ backgroundColor: 'var(--card-bg)', borderColor: 'var(--border-color)' }}>
          <div className="flex items-center justify-between gap-4">
            <div>
              <h3 className="text-base font-extrabold text-slate-900 uppercase tracking-wider">Vocabulary Evidence Table</h3>
              <p className="text-sm text-slate-500 font-medium mt-0.5 flex items-center gap-2">
                <span>Click any row to inspect source image bounding box evidence</span>
                {searchQuery && (
                  <span className="px-2 py-0.5 rounded-full bg-sky-100 text-sky-800 font-bold text-xs border border-sky-200">
                    Showing {filteredWords.length} of {editableWords.length}
                  </span>
                )}
              </p>
            </div>

            <div className="flex items-center gap-3">
              {/* Search Filter Bar */}
              <div className="relative w-64">
                <Search className="w-4 h-4 text-slate-400 absolute left-3 top-1/2 -translate-y-1/2 pointer-events-none" />
                <input 
                  type="text"
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  placeholder="Search word, POS, or meaning..."
                  className="w-full bg-slate-100 border border-slate-300 rounded-xl pl-9 pr-8 py-2 text-xs font-semibold text-slate-900 placeholder-slate-400 focus:outline-none focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20 transition-all shadow-inner"
                />
                {searchQuery && (
                  <button 
                    type="button"
                    onClick={() => setSearchQuery('')}
                    className="absolute right-2.5 top-1/2 -translate-y-1/2 text-slate-400 hover:text-slate-600 p-0.5 rounded-full"
                  >
                    <X className="w-3.5 h-3.5" />
                  </button>
                )}
              </div>

              {isEditing ? (
                <>
                  <button 
                    onClick={handleAddWord}
                    className="px-4 py-2 rounded-xl text-sm font-bold bg-slate-100 border border-slate-300 text-slate-800 hover:bg-slate-200 transition-colors flex items-center gap-1.5"
                  >
                    <Plus className="w-4 h-4" /> Add Item
                  </button>
                  <button 
                    onClick={handleSaveWords}
                    disabled={isSavingWords}
                    className="px-5 py-2 rounded-xl text-sm font-extrabold bg-emerald-600 hover:bg-emerald-700 text-white flex items-center gap-2 shadow-md shadow-emerald-500/20"
                  >
                    {isSavingWords ? <Loader2 className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />}
                    Save Changes
                  </button>
                </>
              ) : (
                <button 
                  onClick={() => setIsEditing(true)}
                  className="px-5 py-2 rounded-xl text-sm font-bold bg-white hover:bg-slate-50 border border-slate-300 text-slate-700 shadow-sm transition-colors flex items-center gap-2"
                >
                  <Edit3 className="w-4 h-4 text-sky-600" /> Edit Words
                </button>
              )}
            </div>
          </div>

          <div className="overflow-x-auto rounded-2xl border border-slate-200 bg-white">
            <table className="w-full text-left">
              <thead className="bg-slate-50 text-slate-700 font-black border-b border-slate-200 uppercase tracking-wider text-xs">
                <tr>
                  <th className="py-2.5 px-4 w-14 text-center">No</th>
                  <th className="py-2.5 px-4 text-xs">English Word</th>
                  <th className="py-2.5 px-4 w-20 text-center text-xs">POS</th>
                  <th className="py-2.5 px-4 text-xs">Korean Meaning</th>
                  <th className="py-2.5 px-4 w-28 text-center text-xs">Evidence</th>
                  {isEditing && <th className="py-2.5 px-4 w-14 text-center text-xs">Actions</th>}
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100 text-slate-900">
                {filteredWords.length === 0 ? (
                  <tr>
                    <td colSpan={6} className="py-12 text-center text-slate-400 font-semibold text-base">
                      {searchQuery ? `No matching words found for "${searchQuery}"` : "No vocabulary words extracted yet. Run conversion to process."}
                    </td>
                  </tr>
                ) : (
                  filteredWords.map((wordItem, idx) => (
                    <tr 
                      key={idx}
                      onClick={() => !isEditing && setSelectedWord(wordItem)}
                      className={`transition-colors cursor-pointer ${
                        selectedWord?.word === wordItem.word 
                          ? 'bg-sky-50/80 font-bold text-sky-900' 
                          : 'hover:bg-slate-50/80'
                      }`}
                    >
                      <td className="py-2.5 px-4 text-center font-mono font-bold text-slate-500 text-sm">{wordItem.no || idx + 1}</td>
                      
                      <td className="py-2.5 px-4">
                        {isEditing ? (
                          <input 
                            type="text" 
                            value={wordItem.word}
                            onChange={(e) => handleWordChange(idx, 'word', e.target.value)}
                            className="w-full bg-white border border-slate-300 rounded-lg px-2.5 py-1.5 text-sm font-bold text-slate-900 focus:outline-none focus:border-sky-500"
                          />
                        ) : (
                          <span className="font-bold text-sm">{wordItem.word}</span>
                        )}
                      </td>

                      <td className="py-2.5 px-4 text-center">
                        {isEditing ? (
                          <div className="flex justify-center">
                            <select
                              value={wordItem.pos || '명'}
                              onChange={(e) => handleWordChange(idx, 'pos', e.target.value)}
                              className="w-7 h-7 rounded-full bg-purple-50 border border-purple-300 text-center font-black text-purple-900 focus:outline-none focus:ring-2 focus:ring-purple-500/30 text-xs shadow-xs cursor-pointer appearance-none flex items-center justify-center pl-1.5"
                              title="품사 선택"
                            >
                              <option value="명">명</option>
                              <option value="동">동</option>
                              <option value="형">형</option>
                              <option value="부">부</option>
                              <option value="전">전</option>
                              <option value="접">접</option>
                              <option value="관">관</option>
                              <option value="감">감</option>
                            </select>
                          </div>
                        ) : (
                          <div className="flex justify-center">
                            <span className="w-6.5 h-6.5 rounded-full bg-purple-100 text-purple-900 font-black border border-purple-300 text-xs flex items-center justify-center shadow-xs">
                              {wordItem.pos}
                            </span>
                          </div>
                        )}
                      </td>

                      <td className="py-2.5 px-4">
                        {isEditing ? (
                          <input 
                            type="text" 
                            value={wordItem.meaning}
                            onChange={(e) => handleWordChange(idx, 'meaning', e.target.value)}
                            className="w-full bg-white border border-slate-300 rounded-lg px-2.5 py-1.5 text-sm font-medium text-slate-900 focus:outline-none focus:border-sky-500"
                          />
                        ) : (
                          <span className="text-sm font-medium text-slate-800">{wordItem.meaning}</span>
                        )}
                      </td>

                      <td className="py-2.5 px-4 text-center">
                        {getSourceImageForWord(wordItem) ? (
                          <CroppedEvidenceThumbnail 
                            imageUrl={`http://localhost:8080/uploads/${getSourceImageForWord(wordItem).imageName}`}
                            bbox={wordItem.bbox || wordItem.bBox}
                            bboxScale={run.bboxScale}
                            imageWidth={wordItem.imageWidth}
                            imageHeight={wordItem.imageHeight}
                            word={wordItem.word}
                            onClick={(e) => { e.stopPropagation(); setSelectedWord(wordItem); }}
                          />
                        ) : (
                          <button 
                            onClick={(e) => { e.stopPropagation(); setSelectedWord(wordItem); }}
                            className="px-3.5 py-2 rounded-xl bg-slate-100 hover:bg-slate-200 text-sky-700 text-xs font-bold flex items-center justify-center gap-1.5 mx-auto transition-colors"
                          >
                            <Eye className="w-4 h-4 text-sky-600" /> View
                          </button>
                        )}
                      </td>

                      {isEditing && (
                        <td className="py-2.5 px-4 text-center">
                          <button 
                            onClick={(e) => { e.stopPropagation(); handleDeleteWord(idx); }}
                            className="p-2 rounded-xl text-rose-600 hover:bg-rose-50 transition-colors"
                          >
                            <Trash2 className="w-4.5 h-4.5" />
                          </button>
                        </td>
                      )}
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </Tabs.Content>

        {/* Tab 2: OCR Transcriptions */}
        <Tabs.Content value="ocr" className="p-7 rounded-3xl border shadow-md space-y-5 focus:outline-none" style={{ backgroundColor: 'var(--card-bg)', borderColor: 'var(--border-color)' }}>
          <h3 className="text-base font-extrabold text-slate-900 uppercase tracking-wider">OCR Image Transcriptions</h3>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-5">
            {run.ocrResults?.map((ocr, idx) => (
              <div key={idx} className="p-5 rounded-2xl bg-slate-50 border border-slate-200 space-y-3">
                <div className="flex items-center justify-between">
                  <span className="text-sm font-bold text-slate-800">{ocr.imageName}</span>
                  <span className="px-2.5 py-0.5 rounded-full text-xs font-bold bg-emerald-100 text-emerald-800 border border-emerald-200">
                    {ocr.status}
                  </span>
                </div>
                <div className="h-48 rounded-xl overflow-hidden bg-white border border-slate-200 relative">
                  <img src={`http://localhost:8080/uploads/${ocr.imageName}`} alt={ocr.imageName} className="w-full h-full object-contain" />
                </div>
                <pre className="p-3.5 rounded-xl bg-white text-xs font-mono text-slate-700 overflow-x-auto max-h-36 border border-slate-200">
                  {ocr.rawText || 'No text extracted'}
                </pre>
              </div>
            ))}
          </div>
        </Tabs.Content>

        {/* Tab 3: Terminal Output Logs */}
        <Tabs.Content value="logs" className="p-7 rounded-3xl border shadow-md space-y-4 focus:outline-none" style={{ backgroundColor: 'var(--card-bg)', borderColor: 'var(--border-color)' }}>
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Terminal className="w-5 h-5 text-sky-600" />
              <h3 className="text-sm font-extrabold text-slate-900 uppercase tracking-wider font-mono">Backend Execution Terminal</h3>
            </div>
            
            <div className="flex items-center gap-3">
              <span className="text-xs text-slate-500 font-mono">{run.logs?.length || 0} Log Lines</span>
              <div className="flex items-center gap-1 bg-slate-100 dark:bg-slate-800/60 p-1 rounded-xl border border-slate-200 dark:border-slate-700">
                <button
                  type="button"
                  onClick={scrollToTop}
                  title="Scroll to Top"
                  className="p-1.5 rounded-lg text-slate-600 dark:text-slate-300 hover:text-sky-600 dark:hover:text-sky-400 hover:bg-white dark:hover:bg-slate-700 transition-all"
                >
                  <ArrowUp className="w-4 h-4" />
                </button>
                <button
                  type="button"
                  onClick={scrollToBottom}
                  title="Scroll to Bottom"
                  className="p-1.5 rounded-lg text-slate-600 dark:text-slate-300 hover:text-sky-600 dark:hover:text-sky-400 hover:bg-white dark:hover:bg-slate-700 transition-all"
                >
                  <ArrowDown className="w-4 h-4" />
                </button>
              </div>
            </div>
          </div>

          <div 
            ref={logContainerRef}
            onScroll={handleLogScroll}
            className="p-4 rounded-2xl bg-slate-950 border border-slate-800 font-mono text-[11px] overflow-y-auto max-h-[480px] space-y-0.5 shadow-inner leading-tight"
          >
            {run.logs?.filter(line => line && line.trim()).map((logLine, idx) => {
              const isError = /❌|Failed|Error|403|500|PERMISSION_DENIED|BLOCKED|disabled/i.test(logLine);
              const isWarning = /⚠️|WARN|Warning/i.test(logLine);
              const isSuccess = /✅|✨|🎉|Success|Completed/i.test(logLine);
              const isInfo = /🚀|🔍|🤖|📷|Processing|Started/i.test(logLine);

              let lineStyle = "text-slate-300";
              let bgStyle = "hover:bg-slate-900/60";
              if (isError) {
                lineStyle = "text-rose-400 font-bold";
                bgStyle = "bg-rose-950/40 border-l-2 border-rose-500 hover:bg-rose-950/60";
              } else if (isWarning) {
                lineStyle = "text-amber-300 font-semibold";
                bgStyle = "bg-amber-950/30 border-l-2 border-amber-500 hover:bg-amber-950/50";
              } else if (isSuccess) {
                lineStyle = "text-emerald-400 font-medium";
              } else if (isInfo) {
                lineStyle = "text-sky-300";
              }

              return (
                <div key={idx} className={`px-2 py-0.5 rounded transition-colors flex items-start gap-2 ${bgStyle}`}>
                  <span className="text-slate-600 select-none text-[10px] shrink-0 w-6 text-right font-mono">{idx + 1}</span>
                  <span className={`break-all ${lineStyle}`}>{logLine}</span>
                </div>
              );
            })}
          </div>
        </Tabs.Content>

      </Tabs.Root>

      {/* Bounding Box Visual Evidence Modal */}
      {selectedWord && getSourceImageForWord(selectedWord) && (
        <InteractiveRedBoxModal
          wordItem={selectedWord}
          words={editableWords}
          bboxScale={run.bboxScale}
          imageUrl={`http://localhost:8080/uploads/${getSourceImageForWord(selectedWord).imageName}`}
          getImageUrl={(w) => {
            const img = getSourceImageForWord(w);
            return img ? `http://localhost:8080/uploads/${img.imageName}` : '';
          }}
          onSelectWord={(w) => setSelectedWord(w)}
          onClose={() => setSelectedWord(null)}
        />
      )}

      {/* ESC Discard Changes Confirmation Modal */}
      {showDiscardModal && (
        <Dialog.Root open={true} onOpenChange={(open) => !open && setShowDiscardModal(false)}>
          <Dialog.Portal>
            <Dialog.Overlay className="fixed inset-0 z-50 bg-slate-950/70 backdrop-blur-sm radix-overlay-anim" />
            <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
              <Dialog.Content className="w-full max-w-md bg-white border border-slate-200 rounded-3xl p-6 shadow-2xl radix-content-anim focus:outline-none">
                <div className="flex items-center gap-3.5 mb-4">
                  <div className="w-11 h-11 rounded-2xl bg-amber-100 border border-amber-200 flex items-center justify-center text-amber-600">
                    <AlertTriangle className="w-6 h-6" />
                  </div>
                  <div>
                    <Dialog.Title className="text-lg font-black text-slate-900">
                      Discard Unsaved Changes?
                    </Dialog.Title>
                    <Dialog.Description className="text-xs font-semibold text-slate-500 mt-0.5">
                      You pressed ESC while editing vocabulary list
                    </Dialog.Description>
                  </div>
                </div>

                <p className="text-sm font-medium text-slate-700 bg-slate-50 p-3.5 rounded-2xl border border-slate-200 mb-5">
                  You have unsaved edits in the vocabulary table. Exiting now will revert all your changes to the original state.
                </p>

                <div className="flex items-center justify-end gap-2.5">
                  <button
                    type="button"
                    onClick={() => setShowDiscardModal(false)}
                    className="px-4 py-2.5 rounded-xl text-xs font-bold bg-slate-100 hover:bg-slate-200 text-slate-700 transition-colors"
                  >
                    Keep Editing
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      if (run && Array.isArray(run.words)) {
                        setEditableWords([...run.words]);
                      }
                      setIsEditing(false);
                      setShowDiscardModal(false);
                    }}
                    className="px-5 py-2.5 rounded-xl text-xs font-black bg-rose-600 hover:bg-rose-700 text-white shadow-md shadow-rose-500/20 transition-all flex items-center gap-1.5"
                  >
                    Discard & Exit
                  </button>
                </div>
              </Dialog.Content>
            </div>
          </Dialog.Portal>
        </Dialog.Root>
      )}

      <DocPreviewModal 
        isOpen={showDocPreview} 
        onClose={() => setShowDocPreview(false)} 
        docData={docPreviewData} 
      />
    </div>
  );
}
