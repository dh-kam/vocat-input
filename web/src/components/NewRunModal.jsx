import React, { useState, useRef, useEffect } from 'react';
import * as Dialog from '@radix-ui/react-dialog';
import { X, Upload, Sparkles, Check, Trash2, Zap } from 'lucide-react';

const FALLBACK_PROVIDERS = [
  {
    id: 'vertex',
    label: 'GCP Vertex',
    desc: 'Google Cloud Vertex AI (Gemini 2.5)',
    defaultModel: 'gemini-2.5-flash',
    models: [
      { id: 'gemini-2.5-flash', label: 'Gemini 2.5 Flash', desc: 'Fast, Balanced (Recommended)', default: true },
      { id: 'gemini-2.5-pro', label: 'Gemini 2.5 Pro', desc: 'Best Accuracy & Deep Reasoning' },
      { id: 'gemini-2.0-flash', label: 'Gemini 2.0 Flash', desc: 'High Throughput' },
      { id: 'gemini-1.5-pro', label: 'Gemini 1.5 Pro', desc: 'Legacy Pro Model' },
      { id: 'gemini-1.5-flash', label: 'Gemini 1.5 Flash', desc: 'Lightweight & Fast' },
    ],
  },
  {
    id: 'bedrock',
    label: 'AWS Bedrock',
    desc: 'Amazon Bedrock (Claude 4.6 & Nova)',
    defaultModel: 'us.anthropic.claude-sonnet-4-6',
    models: [
      { id: 'us.anthropic.claude-sonnet-4-6', label: 'Claude 4.6 Sonnet', desc: 'State-of-the-art AI (Recommended)', default: true },
      { id: 'us.anthropic.claude-3-7-sonnet-20250219-v1:0', label: 'Claude 3.7 Sonnet', desc: 'Hybrid Reasoning & Vision' },
      { id: 'us.anthropic.claude-3-5-sonnet-20241022-v2:0', label: 'Claude 3.5 Sonnet v2', desc: 'High Performance Multimodal' },
      { id: 'amazon.nova-pro-v1:0', label: 'Nova Pro', desc: 'Higher Accuracy Reasoning' },
      { id: 'amazon.nova-lite-v1:0', label: 'Nova Lite', desc: 'Fast, Cost-Effective' },
    ],
  },
];

export default function NewRunModal({ isOpen, onClose, onSubmit }) {
  const [dragActive, setDragActive] = useState(false);
  const [selectedFiles, setSelectedFiles] = useState([]);
  const [providers, setProviders] = useState(FALLBACK_PROVIDERS);
  const [ocrProvider, setOcrProvider] = useState('vertex');
  const [ocrModel, setOcrModel] = useState('gemini-2.5-flash');
  const [preserveOrder, setPreserveOrder] = useState(true);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const fileInputRef = useRef(null);

  useEffect(() => {
    if (!isOpen) return;
    fetch('/api/models', { credentials: 'include' })
      .then(res => res.ok ? res.json() : null)
      .then(data => {
        if (data && Array.isArray(data.providers) && data.providers.length > 0) {
          setProviders(data.providers);
          const currentP = data.providers.find(p => p.id === ocrProvider) || data.providers[0];
          if (currentP) {
            const hasModel = currentP.models?.some(m => m.id === ocrModel);
            if (!hasModel) {
              setOcrModel(currentP.defaultModel || currentP.models?.[0]?.id || 'gemini-2.5-flash');
            }
          }
        }
      })
      .catch(() => {
        // use fallback providers
      });
  }, [isOpen]);

  const currentProviderObj = providers.find(p => p.id === ocrProvider) || providers[0];
  const availableModels = currentProviderObj?.models || [];

  const handleProviderChange = (providerId) => {
    setOcrProvider(providerId);
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

    const formData = new FormData();
    selectedFiles.forEach(item => formData.append('images', item.file));
    formData.append('ocrProvider', ocrProvider);
    formData.append('ocrModel', ocrModel);
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

                {/* Provider Picker */}
                <div className="space-y-4">
                  <div>
                    <label className="block text-xs font-extrabold uppercase text-slate-500 mb-2 tracking-wider">OCR Engine Provider</label>
                    <div className="grid grid-cols-2 gap-2">
                      {providers.map(p => (
                        <button
                          key={p.id}
                          type="button"
                          onClick={() => handleProviderChange(p.id)}
                          className={`p-3 sm:p-3.5 rounded-xl border text-left text-xs font-black transition-all cursor-pointer ${
                            ocrProvider === p.id 
                              ? 'border-sky-500 bg-sky-50 text-sky-800 ring-2 ring-sky-500/20 shadow-sm' 
                              : 'border-slate-300 bg-white text-slate-600 hover:border-slate-400'
                          }`}
                        >
                          <div>{p.label}</div>
                          {p.desc && <div className="text-[10px] font-normal text-slate-500 mt-0.5 truncate">{p.desc}</div>}
                        </button>
                      ))}
                    </div>
                  </div>

                  {/* Model Selector */}
                  <div>
                    <label className="block text-xs font-extrabold uppercase text-slate-500 mb-2 tracking-wider">OCR Model</label>
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                      {availableModels.map(m => (
                        <button
                          key={m.id}
                          type="button"
                          onClick={() => setOcrModel(m.id)}
                          className={`p-3 rounded-xl border text-left transition-all cursor-pointer ${
                            ocrModel === m.id
                              ? 'border-sky-500 bg-sky-50 ring-2 ring-sky-500/20'
                              : 'border-slate-300 bg-white hover:border-slate-400'
                          }`}
                        >
                          <div className="text-xs font-black text-slate-900">{m.label}</div>
                          <div className="text-[10px] font-medium text-slate-500 mt-0.5">{m.desc}</div>
                        </button>
                      ))}
                    </div>
                  </div>

                  {/* Preserve Order */}
                  <div>
                    <label className="block text-xs font-extrabold uppercase text-slate-500 mb-2 tracking-wider">Sequence Order</label>
                    <button
                      type="button"
                      onClick={() => setPreserveOrder(!preserveOrder)}
                      className={`w-full p-3 sm:p-3.5 rounded-xl border flex items-center justify-between text-xs font-black transition-all cursor-pointer ${
                        preserveOrder ? 'border-sky-500 bg-sky-50 text-sky-800 ring-2 ring-sky-500/20' : 'border-slate-300 bg-white text-slate-600'
                      }`}
                    >
                      <span>Preserve Original Order</span>
                      <div className={`w-4.5 h-4.5 rounded flex items-center justify-center border ${preserveOrder ? 'bg-sky-600 border-sky-600 text-white' : 'border-slate-400'}`}>
                        {preserveOrder && <Check className="w-3.5 h-3.5 stroke-[3]" />}
                      </div>
                    </button>
                  </div>
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
