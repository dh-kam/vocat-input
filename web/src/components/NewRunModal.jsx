import React, { useState, useRef, useEffect } from 'react';
import * as Dialog from '@radix-ui/react-dialog';
import { X, Upload, Sparkles, Check, Trash2, Zap, Cpu, RefreshCw, ChevronDown, Layers, Edit3 } from 'lucide-react';

const FALLBACK_PROVIDERS = [
  {
    id: 'google-ai-studio',
    label: 'Google AI Studio',
    desc: 'Google AI Studio Gemini API (Direct Key)',
    defaultModel: 'gemini-2.5-flash',
    models: [
      { id: 'gemini-2.5-flash', label: 'Gemini 2.5 Flash', desc: 'Fast, Multimodal & Balanced (Recommended)', default: true },
      { id: 'gemini-2.5-pro', label: 'Gemini 2.5 Pro', desc: 'Deep Reasoning & Highest Accuracy' },
      { id: 'gemini-3.7-flash', label: 'Gemini 3.7 Flash', desc: 'Next-gen Flagship Multimodal (Fast & Accurate)' },
      { id: 'gemini-3.1-pro-preview', label: 'Gemini 3.1 Pro Preview', desc: 'Advanced Next-gen Pro Reasoning' },
      { id: 'gemini-2.5-flash-lite', label: 'Gemini 2.5 Flash Lite', desc: 'Ultra-fast Lightweight' },
      { id: 'claude-sonnet-4-6', label: 'Claude 4.6 Sonnet', desc: 'State-of-the-art AI Multimodal' },
      { id: 'claude-opus-4-6', label: 'Claude 4.6 Opus', desc: 'Ultimate Reasoning & Deep Context' },
      { id: 'claude-4-5-fable', label: 'Claude 4.5 Fable', desc: 'Creative & Nuanced Context Analysis' },
      { id: 'claude-sonnet-4-5', label: 'Claude 4.5 Sonnet', desc: 'High Performance Multimodal' },
    ],
  },
  {
    id: 'vertex',
    label: 'GCP Vertex',
    desc: 'Google Cloud Vertex AI (Gemini & Claude)',
    defaultModel: 'gemini-2.5-flash',
    models: [
      { id: 'gemini-2.5-flash', label: 'Gemini 2.5 Flash', desc: 'Fast, Multimodal & High Accuracy (Recommended)', default: true },
      { id: 'gemini-2.5-pro', label: 'Gemini 2.5 Pro', desc: 'Deep Reasoning & Highest OCR Accuracy' },
      { id: 'gemini-3.7-flash', label: 'Gemini 3.7 Flash', desc: 'Next-gen Flagship Multimodal (Fast & Accurate)' },
      { id: 'gemini-3.1-pro-preview', label: 'Gemini 3.1 Pro Preview', desc: 'Advanced Next-gen Pro Reasoning' },
      { id: 'gemini-2.5-flash-lite', label: 'Gemini 2.5 Flash Lite', desc: 'Ultra-fast Lightweight' },
      { id: 'claude-sonnet-4-6', label: 'Claude 4.6 Sonnet', desc: 'State-of-the-art AI Multimodal' },
      { id: 'claude-opus-4-6', label: 'Claude 4.6 Opus', desc: 'Ultimate Reasoning & Deep Context' },
      { id: 'claude-4-5-fable', label: 'Claude 4.5 Fable', desc: 'Creative & Nuanced Context Analysis' },
      { id: 'claude-sonnet-4-5', label: 'Claude 4.5 Sonnet', desc: 'High Performance Multimodal' },
    ],
  },
  {
    id: 'bedrock',
    label: 'AWS Bedrock',
    desc: 'Amazon Bedrock (Claude 4.6, 4.5 & Nova)',
    defaultModel: 'us.anthropic.claude-sonnet-4-6',
    models: [
      { id: 'us.anthropic.claude-sonnet-4-6', label: 'Claude 4.6 Sonnet', desc: 'State-of-the-art AI (Recommended)', default: true },
      { id: 'us.anthropic.claude-opus-4-6', label: 'Claude 4.6 Opus', desc: 'Ultimate Reasoning & Deep Context' },
      { id: 'us.anthropic.claude-4-5-fable', label: 'Claude 4.5 Fable', desc: 'Creative & Nuanced Context Analysis' },
      { id: 'us.anthropic.claude-sonnet-4-5-20250929-v1:0', label: 'Claude 4.5 Sonnet', desc: 'High Performance Multimodal' },
      { id: 'amazon.nova-pro-v1:0', label: 'Nova Pro', desc: 'Higher Accuracy Reasoning' },
      { id: 'amazon.nova-lite-v1:0', label: 'Nova Lite', desc: 'Fast, Cost-Effective' },
    ],
  },
];

export default function NewRunModal({ isOpen, onClose, onSubmit }) {
  const [dragActive, setDragActive] = useState(false);
  const [selectedFiles, setSelectedFiles] = useState([]);
  const [providers, setProviders] = useState(FALLBACK_PROVIDERS);
  const [ocrProvider, setOcrProvider] = useState('google-ai-studio');
  const [ocrModel, setOcrModel] = useState('gemini-2.5-flash');
  const [customModelMode, setCustomModelMode] = useState(false);
  const [customModelInput, setCustomModelInput] = useState('');
  const [isLoadingModels, setIsLoadingModels] = useState(false);
  const [preserveOrder, setPreserveOrder] = useState(true);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const fileInputRef = useRef(null);

  const fetchModels = async (forceRefresh = false) => {
    setIsLoadingModels(true);
    try {
      const url = `/api/models${forceRefresh ? '?refresh=true' : ''}`;
      const res = await fetch(url, { credentials: 'include' });
      if (res.ok) {
        const data = await res.json();
        if (data && Array.isArray(data.providers) && data.providers.length > 0) {
          setProviders(data.providers);
          const currentP = data.providers.find(p => p.id === ocrProvider) || data.providers[0];
          if (currentP) {
            const hasModel = currentP.models?.some(m => m.id === ocrModel);
            if (!hasModel && !customModelMode) {
              setOcrModel(currentP.defaultModel || currentP.models?.[0]?.id || 'gemini-2.5-flash');
            }
          }
        }
      }
    } catch (err) {
      console.warn('Could not fetch dynamic models, using local catalog:', err);
    } finally {
      setIsLoadingModels(false);
    }
  };

  useEffect(() => {
    if (!isOpen) return;
    fetchModels(false);
  }, [isOpen]);

  const currentProviderObj = providers.find(p => p.id === ocrProvider) || providers[0];
  const availableModels = currentProviderObj?.models || [];
  const currentModelObj = availableModels.find(m => m.id === ocrModel);

  const handleProviderChange = (providerId) => {
    setOcrProvider(providerId);
    setCustomModelMode(false);
    const targetProvider = providers.find(p => p.id === providerId);
    if (targetProvider) {
      setOcrModel(targetProvider.defaultModel || targetProvider.models?.[0]?.id || '');
    }
  };

  const handleFiles = (files) => {
    const valid = Array.from(files).filter(f => f.type.startsWith('image/'));
    const mapped = valid.map(f => ({
      file: f,
      name: f.name,
      size: (f.size / 1024).toFixed(1) + ' KB',
      preview: URL.createObjectURL(f)
    }));
    setSelectedFiles(prev => [...prev, ...mapped]);
  };

  const handleDrag = (e) => {
    e.preventDefault();
    e.stopPropagation();
    if (e.type === 'dragenter' || e.type === 'dragover') {
      setDragActive(true);
    } else if (e.type === 'dragleave') {
      setDragActive(false);
    }
  };

  const handleDrop = (e) => {
    e.preventDefault();
    e.stopPropagation();
    setDragActive(false);
    if (e.dataTransfer.files && e.dataTransfer.files[0]) {
      handleFiles(e.dataTransfer.files);
    }
  };

  const removeFile = (idx) => {
    URL.revokeObjectURL(selectedFiles[idx]?.preview);
    setSelectedFiles(prev => prev.filter((_, i) => i !== idx));
  };

  const handleSubmit = async (e) => {
    e.preventDefault();
    if (selectedFiles.length === 0) return;
    setIsSubmitting(true);

    const effectiveModel = customModelMode && customModelInput.trim() ? customModelInput.trim() : ocrModel;

    const formData = new FormData();
    selectedFiles.forEach(item => formData.append('images', item.file));
    formData.append('ocrProvider', ocrProvider);
    formData.append('ocrModel', effectiveModel);
    formData.append('preserveOrder', preserveOrder ? 'true' : 'false');

    await onSubmit(formData);
    setIsSubmitting(false);
    setSelectedFiles([]);
  };

  return (
    <Dialog.Root open={isOpen} onOpenChange={(open) => !open && onClose()}>
      <Dialog.Portal>
        {/* Radix UI Overlay */}
        <Dialog.Overlay className="fixed inset-0 z-50 bg-slate-950/60 backdrop-blur-sm radix-overlay-anim" />

        {/* Flex Centering Container to prevent initial layout shift */}
        <div className="fixed inset-0 z-50 flex items-center justify-center p-0 sm:p-4">
          <Dialog.Content className="w-full max-w-2xl max-h-[100dvh] sm:max-h-[88vh] bg-white border border-slate-200 rounded-none sm:rounded-3xl shadow-2xl overflow-hidden glass-panel radix-content-anim focus:outline-none flex flex-col h-full sm:h-auto sm:my-auto">
            
            {/* Header (Fixed) */}
            <div className="flex items-center justify-between px-5 sm:px-7 py-4 sm:py-5 border-b border-slate-200 bg-slate-50/70 shrink-0">
              <div className="flex items-center gap-3 sm:gap-3.5">
                <div className="w-10 h-10 sm:w-11 sm:h-11 rounded-2xl bg-sky-100 border border-sky-200 flex items-center justify-center text-sky-700 shadow-inner shrink-0">
                  <Sparkles className="w-5 h-5 sm:w-6 sm:h-6" />
                </div>
                <div>
                  <Dialog.Title className="text-lg sm:text-xl font-extrabold text-slate-900 tracking-tight">
                    Create New Vocat Run
                  </Dialog.Title>
                  <p className="text-[11px] sm:text-xs font-semibold text-slate-500 mt-0.5">Multi-Cloud AI Vision OCR Workflow Setup</p>
                </div>
              </div>
              <Dialog.Close className="w-8 h-8 sm:w-9 sm:h-9 rounded-full bg-slate-100 hover:bg-slate-200 border border-slate-300 flex items-center justify-center text-slate-600 hover:text-slate-900 transition-colors">
                <X className="w-4 h-4 sm:w-4.5 sm:h-4.5" />
              </Dialog.Close>
            </div>

            <form onSubmit={handleSubmit} className="flex flex-col flex-1 min-h-0 overflow-hidden">
              
              {/* Scrollable Form Content */}
              <div className="flex-1 overflow-y-auto p-5 sm:p-7 space-y-5 sm:space-y-6 overscroll-contain">
                {/* Drag & Drop Area */}
                <div
                  onDragEnter={handleDrag}
                  onDragOver={handleDrag}
                  onDragLeave={handleDrag}
                  onDrop={handleDrop}
                  onClick={() => fileInputRef.current?.click()}
                  className={`relative flex flex-col items-center justify-center p-6 sm:p-8 border-2 border-dashed rounded-2xl cursor-pointer transition-all duration-200 ${
                    dragActive 
                      ? 'border-sky-500 bg-sky-50 scale-[1.01]' 
                      : 'border-slate-300 hover:border-sky-400 bg-slate-50/60 hover:bg-slate-100/60'
                  }`}
                >
                  <input 
                    ref={fileInputRef}
                    type="file" 
                    multiple 
                    accept="image/*" 
                    className="hidden" 
                    onChange={(e) => e.target.files && handleFiles(e.target.files)} 
                  />
                  <div className="w-12 h-12 sm:w-14 sm:h-14 mb-2.5 sm:mb-3 rounded-2xl bg-white border border-slate-200 shadow-sm flex items-center justify-center text-sky-600">
                    <Upload className="w-6 h-6 sm:w-7 sm:h-7" />
                  </div>
                  <p className="text-sm sm:text-base font-extrabold text-slate-900 text-center">
                    Drop vocabulary images here, or <span className="text-sky-600 underline">browse files</span>
                  </p>
                  <p className="mt-1 text-[11px] sm:text-xs font-semibold text-slate-500">JPG, PNG, WEBP supported</p>
                </div>

                {/* Selected File Grid */}
                {selectedFiles.length > 0 && (
                  <div className="space-y-2.5">
                    <div className="flex justify-between items-center px-1">
                      <span className="text-xs font-bold uppercase text-slate-500 tracking-wider">
                        Attached Files ({selectedFiles.length})
                      </span>
                      <button 
                        type="button" 
                        onClick={() => {
                          selectedFiles.forEach(f => URL.revokeObjectURL(f.preview));
                          setSelectedFiles([]);
                        }}
                        className="text-xs font-bold text-rose-600 hover:text-rose-700 cursor-pointer"
                      >
                        Clear all
                      </button>
                    </div>
                    <div className="max-h-44 overflow-y-auto space-y-2 pr-1">
                      {selectedFiles.map((fileItem, idx) => (
                        <div key={idx} className="flex items-center justify-between p-3 rounded-xl bg-white border border-slate-200 shadow-sm">
                          <div className="flex items-center gap-3 overflow-hidden">
                            <img src={fileItem.preview} alt={fileItem.name} className="w-11 h-11 object-cover rounded-lg border border-slate-200 shrink-0" />
                            <div className="min-w-0">
                              <p className="text-sm font-bold text-slate-900 truncate">{fileItem.name}</p>
                              <p className="text-xs text-slate-500 font-medium">{fileItem.size}</p>
                            </div>
                          </div>
                          <button 
                            type="button" 
                            onClick={(e) => { e.stopPropagation(); removeFile(idx); }}
                            className="p-2 rounded-lg text-slate-400 hover:text-rose-600 hover:bg-rose-50 cursor-pointer"
                          >
                            <Trash2 className="w-4.5 h-4.5" />
                          </button>
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                {/* AI Provider & Dynamic Model Dual Combobox Selection */}
                <div className="rounded-2xl p-4 sm:p-5 border border-slate-200 bg-slate-50/60 shadow-xs space-y-4">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <div className="w-7 h-7 rounded-lg bg-sky-100 border border-sky-200 flex items-center justify-center text-sky-700">
                        <Cpu className="w-4 h-4" />
                      </div>
                      <div>
                        <h4 className="text-xs font-black uppercase text-slate-800 tracking-wider">AI Engine & Model Selection</h4>
                        <p className="text-[10px] text-slate-500 font-medium">Select Cloud Provider & Dynamically Loaded OCR Model</p>
                      </div>
                    </div>
                    <button
                      type="button"
                      onClick={() => fetchModels(true)}
                      disabled={isLoadingModels}
                      className="text-[11px] font-bold text-sky-700 hover:text-sky-800 flex items-center gap-1.5 px-2.5 py-1 rounded-lg bg-white hover:bg-sky-50 border border-slate-200 hover:border-sky-300 shadow-xs transition-all cursor-pointer disabled:opacity-50"
                      title="Fetch live model list from cloud"
                    >
                      <RefreshCw className={`w-3.5 h-3.5 ${isLoadingModels ? 'animate-spin text-sky-600' : ''}`} />
                      <span>{isLoadingModels ? 'Refreshing...' : 'Live Fetch'}</span>
                    </button>
                  </div>

                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-3.5 sm:gap-4">
                    {/* Left: Provider Combobox */}
                    <div>
                      <label className="block text-[11px] font-extrabold uppercase text-slate-600 mb-1.5 tracking-wider flex items-center justify-between">
                        <span>Provider</span>
                        <span className="text-[10px] text-sky-600 font-bold">{providers.length} engines</span>
                      </label>
                      <div className="relative">
                        <select
                          value={ocrProvider}
                          onChange={(e) => handleProviderChange(e.target.value)}
                          className="w-full bg-white border border-slate-300 hover:border-slate-400 focus:border-sky-500 rounded-xl px-3.5 py-2.5 text-xs sm:text-sm font-bold text-slate-900 focus:outline-none focus:ring-2 focus:ring-sky-500/20 shadow-xs appearance-none cursor-pointer pr-9 transition-colors"
                        >
                          {providers.map(p => (
                            <option key={p.id} value={p.id}>
                              {p.label} {p.id === 'google-ai-studio' ? '⚡' : p.id === 'bedrock' ? '🟧' : '☁️'}
                            </option>
                          ))}
                        </select>
                        <ChevronDown className="w-4 h-4 text-slate-400 absolute right-3 top-1/2 -translate-y-1/2 pointer-events-none" />
                      </div>
                      {currentProviderObj?.desc && (
                        <p className="text-[10px] text-slate-500 font-medium mt-1 truncate pl-1">
                          {currentProviderObj.desc}
                        </p>
                      )}
                    </div>

                    {/* Right: Model Combobox */}
                    <div>
                      <label className="block text-[11px] font-extrabold uppercase text-slate-600 mb-1.5 tracking-wider flex items-center justify-between">
                        <span>Model</span>
                        <span className="text-[10px] text-emerald-600 font-bold">{availableModels.length} models</span>
                      </label>

                      {!customModelMode ? (
                        <div className="relative">
                          <select
                            value={ocrModel}
                            onChange={(e) => {
                              if (e.target.value === '__custom__') {
                                setCustomModelMode(true);
                                setCustomModelInput(ocrModel);
                              } else {
                                setOcrModel(e.target.value);
                              }
                            }}
                            className="w-full bg-white border border-slate-300 hover:border-slate-400 focus:border-sky-500 rounded-xl px-3.5 py-2.5 text-xs sm:text-sm font-bold text-slate-900 focus:outline-none focus:ring-2 focus:ring-sky-500/20 shadow-xs appearance-none cursor-pointer pr-9 transition-colors"
                          >
                            {availableModels.map(m => (
                              <option key={m.id} value={m.id}>
                                {m.label} {m.default ? '★' : ''} ({m.id})
                              </option>
                            ))}
                            <option value="__custom__">✍️ Custom Model ID...</option>
                          </select>
                          <ChevronDown className="w-4 h-4 text-slate-400 absolute right-3 top-1/2 -translate-y-1/2 pointer-events-none" />
                        </div>
                      ) : (
                        <div className="flex items-center gap-1.5">
                          <input
                            type="text"
                            value={customModelInput}
                            onChange={(e) => setCustomModelInput(e.target.value)}
                            placeholder="Enter model ID (e.g. gemini-2.5-pro)"
                            className="flex-1 bg-white border border-sky-400 rounded-xl px-3.5 py-2 text-xs sm:text-sm font-bold text-slate-900 focus:outline-none focus:ring-2 focus:ring-sky-500/20 shadow-xs"
                            autoFocus
                          />
                          <button
                            type="button"
                            onClick={() => {
                              setCustomModelMode(false);
                              if (customModelInput.trim()) {
                                setOcrModel(customModelInput.trim());
                              }
                            }}
                            className="px-2.5 py-2 rounded-xl bg-sky-100 hover:bg-sky-200 text-sky-800 text-xs font-bold shrink-0 cursor-pointer"
                          >
                            Set
                          </button>
                          <button
                            type="button"
                            onClick={() => setCustomModelMode(false)}
                            className="px-2.5 py-2 rounded-xl bg-slate-100 hover:bg-slate-200 text-slate-600 text-xs font-bold shrink-0 cursor-pointer"
                          >
                            List
                          </button>
                        </div>
                      )}

                      {currentModelObj?.desc && !customModelMode && (
                        <p className="text-[10px] text-slate-500 font-medium mt-1 truncate pl-1" title={currentModelObj.desc}>
                          {currentModelObj.desc}
                        </p>
                      )}
                    </div>
                  </div>
                </div>

                {/* Preserve Order */}
                <div>
                  <button
                    type="button"
                    onClick={() => setPreserveOrder(!preserveOrder)}
                    className={`w-full p-3 sm:p-3.5 rounded-xl border flex items-center justify-between text-xs font-black transition-all cursor-pointer ${
                      preserveOrder ? 'border-sky-500 bg-sky-50 text-sky-800 ring-2 ring-sky-500/20' : 'border-slate-300 bg-white text-slate-600'
                    }`}
                  >
                    <span>Preserve Original Sequence Order</span>
                    <div className={`w-4.5 h-4.5 rounded flex items-center justify-center border ${preserveOrder ? 'bg-sky-600 border-sky-600 text-white' : 'border-slate-400'}`}>
                      {preserveOrder && <Check className="w-3.5 h-3.5 stroke-[3]" />}
                    </div>
                  </button>
                </div>
              </div>

              {/* Action Buttons (Fixed Footer) */}
              <div className="flex items-center justify-end gap-2.5 sm:gap-3 px-5 sm:px-7 py-3.5 sm:py-4 border-t border-slate-200 bg-slate-50/80 shrink-0">
                <Dialog.Close className="px-4 sm:px-5 py-2 sm:py-2.5 rounded-xl border border-slate-300 bg-white hover:bg-slate-50 text-slate-700 text-xs sm:text-sm font-bold shadow-sm cursor-pointer">
                  Cancel
                </Dialog.Close>
                <button
                  type="submit"
                  disabled={selectedFiles.length === 0 || isSubmitting}
                  className="px-5 sm:px-6 py-2 sm:py-2.5 rounded-xl bg-gradient-to-r from-sky-600 to-indigo-600 hover:from-sky-700 hover:to-indigo-700 text-white text-xs sm:text-sm font-extrabold flex items-center gap-2 shadow-lg shadow-sky-500/25 disabled:opacity-50 transition-all cursor-pointer"
                >
                  {isSubmitting ? <Zap className="w-4 h-4 animate-spin" /> : <Sparkles className="w-4 h-4" />}
                  Start Vocat Conversion
                </button>
              </div>

            </form>

          </Dialog.Content>
        </div>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
