import React, { useState, useRef } from 'react';
import * as Dialog from '@radix-ui/react-dialog';
import { X, Upload, Sparkles, Check, Trash2, Zap } from 'lucide-react';

export default function NewRunModal({ isOpen, onClose, onSubmit }) {
  const [dragActive, setDragActive] = useState(false);
  const [selectedFiles, setSelectedFiles] = useState([]);
  const [ocrProvider, setOcrProvider] = useState('vertex');
  const [ocrModel, setOcrModel] = useState('gemini-2.5-flash');
  const [preserveOrder, setPreserveOrder] = useState(true);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const fileInputRef = useRef(null);

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
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
          <Dialog.Content className="w-full max-w-2xl bg-white border border-slate-200 rounded-3xl shadow-2xl overflow-hidden glass-panel radix-content-anim focus:outline-none my-auto">
            
            {/* Header */}
            <div className="flex items-center justify-between px-7 py-5 border-b border-slate-200 bg-slate-50/50">
              <div className="flex items-center gap-3.5">
                <div className="w-11 h-11 rounded-2xl bg-sky-100 border border-sky-200 flex items-center justify-center text-sky-700 shadow-inner">
                  <Sparkles className="w-6 h-6" />
                </div>
                <div>
                  <Dialog.Title className="text-xl font-extrabold text-slate-900 tracking-tight">
                    Create New Vocat Run
                  </Dialog.Title>
                  <p className="text-xs font-semibold text-slate-500 mt-0.5">Multi-Cloud AI Vision OCR Workflow Setup</p>
                </div>
              </div>
              <Dialog.Close className="w-9 h-9 rounded-full bg-slate-100 hover:bg-slate-200 border border-slate-300 flex items-center justify-center text-slate-600 hover:text-slate-900 transition-colors">
                <X className="w-4.5 h-4.5" />
              </Dialog.Close>
            </div>

            <form onSubmit={handleSubmit} className="p-7 space-y-6">
              
              {/* Drag & Drop Area */}
              <div
                onDragEnter={handleDrag}
                onDragOver={handleDrag}
                onDragLeave={handleDrag}
                onDrop={handleDrop}
                onClick={() => fileInputRef.current?.click()}
                className={`relative flex flex-col items-center justify-center p-8 border-2 border-dashed rounded-2xl cursor-pointer transition-all duration-200 ${
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
                <div className="w-14 h-14 mb-3 rounded-2xl bg-white border border-slate-200 shadow-sm flex items-center justify-center text-sky-600">
                  <Upload className="w-7 h-7" />
                </div>
                <p className="text-base font-extrabold text-slate-900">
                  Drop vocabulary images here, or <span className="text-sky-600 underline">browse files</span>
                </p>
                <p className="mt-1 text-xs font-semibold text-slate-500">JPG, PNG, WEBP supported</p>
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
                      className="text-xs font-bold text-rose-600 hover:text-rose-700"
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
                          className="p-2 rounded-lg text-slate-400 hover:text-rose-600 hover:bg-rose-50"
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
                    {[
                      { id: 'vertex', label: 'GCP Vertex' },
                      { id: 'bedrock', label: 'AWS Bedrock' }
                    ].map(p => (
                      <button
                        key={p.id}
                        type="button"
                        onClick={() => {
                          setOcrProvider(p.id);
                          setOcrModel(p.id === 'bedrock' ? 'us.anthropic.claude-sonnet-4-6' : 'gemini-2.5-flash');
                        }}
                        className={`p-3.5 rounded-xl border text-left text-xs font-black transition-all cursor-pointer ${
                          ocrProvider === p.id 
                            ? 'border-sky-500 bg-sky-50 text-sky-800 ring-2 ring-sky-500/20 shadow-sm' 
                            : 'border-slate-300 bg-white text-slate-600 hover:border-slate-400'
                        }`}
                      >
                        {p.label}
                      </button>
                    ))}
                  </div>
                </div>

                {/* Model Selector */}
                <div>
                  <label className="block text-xs font-extrabold uppercase text-slate-500 mb-2 tracking-wider">OCR Model</label>
                  <div className="grid grid-cols-2 gap-2">
                    {(ocrProvider === 'bedrock' ? [
                      { id: 'us.anthropic.claude-sonnet-4-6', label: 'Claude 4.6 Sonnet', desc: 'State-of-the-art AI (Recommended)' },
                      { id: 'us.anthropic.claude-sonnet-4-5-20250929-v1:0', label: 'Claude 4.5 Sonnet', desc: 'Top multimodal AI' },
                      { id: 'amazon.nova-pro-v1:0', label: 'Nova Pro', desc: 'Higher accuracy' },
                      { id: 'amazon.nova-lite-v1:0', label: 'Nova Lite', desc: 'Fast, cost-effective' },
                    ] : [
                      { id: 'gemini-2.5-flash', label: 'Gemini 2.5 Flash', desc: 'Fast, balanced' },
                      { id: 'gemini-2.5-pro', label: 'Gemini 2.5 Pro', desc: 'Best accuracy' },
                      { id: 'claude-sonnet-4-6', label: 'Claude 4.6 Sonnet', desc: 'Anthropic Model Garden' },
                    ]).map(m => (
                      <button
                        key={m.id}
                        type="button"
                        onClick={() => setOcrModel(m.id)}
                        className={`p-3 rounded-xl border text-left transition-all ${
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
                    className={`w-full p-3.5 rounded-xl border flex items-center justify-between text-xs font-black transition-all ${
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

              {/* Action Buttons */}
              <div className="flex items-center justify-end gap-3 pt-4 border-t border-slate-200">
                <Dialog.Close className="px-5 py-2.5 rounded-xl border border-slate-300 bg-white hover:bg-slate-50 text-slate-700 text-sm font-bold shadow-sm">
                  Cancel
                </Dialog.Close>
                <button
                  type="submit"
                  disabled={selectedFiles.length === 0 || isSubmitting}
                  className="px-6 py-2.5 rounded-xl bg-gradient-to-r from-sky-600 to-indigo-600 hover:from-sky-700 hover:to-indigo-700 text-white text-sm font-extrabold flex items-center gap-2 shadow-lg shadow-sky-500/25 disabled:opacity-50 transition-all"
                >
                  {isSubmitting ? <Zap className="w-4.5 h-4.5 animate-spin" /> : <Sparkles className="w-4.5 h-4.5" />}
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
