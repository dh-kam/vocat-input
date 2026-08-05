import React from 'react';
import * as Dialog from '@radix-ui/react-dialog';
import { X, FileText, Database } from 'lucide-react';

export default function DocPreviewModal({ isOpen, onClose, docData }) {
  if (!docData) return null;

  const { vocabulary, corpusList } = docData;

  return (
    <Dialog.Root open={isOpen} onOpenChange={(open) => !open && onClose()}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-50 bg-slate-950/70 backdrop-blur-sm radix-overlay-anim" />
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
          <Dialog.Content className="w-full max-w-4xl max-h-[90vh] bg-white border border-slate-200 rounded-3xl shadow-2xl flex flex-col radix-content-anim focus:outline-none overflow-hidden">
            
            <div className="px-6 py-5 border-b border-slate-100 flex items-center justify-between shrink-0 bg-slate-50/50">
              <div className="flex items-center gap-3">
                <div className="w-10 h-10 rounded-2xl bg-sky-100 flex items-center justify-center">
                  <FileText className="w-5 h-5 text-sky-600" />
                </div>
                <div>
                  <Dialog.Title className="text-lg font-black text-slate-900">
                    Doc Preview
                  </Dialog.Title>
                  <Dialog.Description className="text-xs font-semibold text-slate-500 mt-0.5">
                    VocatBook Format: {vocabulary?.name} ({vocabulary?.total} words)
                  </Dialog.Description>
                </div>
              </div>
              <button 
                type="button"
                onClick={onClose}
                className="w-10 h-10 rounded-full flex items-center justify-center text-slate-400 hover:text-slate-600 hover:bg-slate-100 transition-colors cursor-pointer"
              >
                <X className="w-5 h-5" />
              </button>
            </div>

            <div className="flex-1 overflow-auto p-6 bg-slate-50/30">
              
              <div className="mb-6 bg-white p-5 rounded-2xl border border-slate-200 shadow-sm">
                <h4 className="text-sm font-bold text-slate-800 mb-3 flex items-center gap-2">
                  <Database className="w-4 h-4 text-slate-400" />
                  Vocabulary Metadata
                </h4>
                <div className="grid grid-cols-2 gap-4 text-sm">
                  <div>
                    <span className="text-slate-400 font-semibold block text-xs">Vocabulary ID</span>
                    <span className="font-mono text-slate-700">{vocabulary?.id}</span>
                  </div>
                  <div>
                    <span className="text-slate-400 font-semibold block text-xs">Bookcase ID</span>
                    <span className="font-mono text-slate-700">{vocabulary?.bookcaseId}</span>
                  </div>
                  <div>
                    <span className="text-slate-400 font-semibold block text-xs">Created At</span>
                    <span className="text-slate-700">{vocabulary?.createdAt}</span>
                  </div>
                  <div>
                    <span className="text-slate-400 font-semibold block text-xs">Languages</span>
                    <span className="text-slate-700">{vocabulary?.wordLang} &rarr; {vocabulary?.meaningLang}</span>
                  </div>
                </div>
              </div>

              <div className="bg-white border border-slate-200 rounded-2xl overflow-hidden shadow-sm">
                <table className="w-full text-left text-sm text-slate-600">
                  <thead className="bg-slate-50 border-b border-slate-200 text-xs uppercase font-bold text-slate-500">
                    <tr>
                      <th className="px-4 py-3 w-12 text-center">No</th>
                      <th className="px-4 py-3">Word</th>
                      <th className="px-4 py-3 w-16 text-center">POS</th>
                      <th className="px-4 py-3">Meaning</th>
                      <th className="px-4 py-3">ID</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-100">
                    {(corpusList || []).map((item, idx) => (
                      <tr key={item.id} className="hover:bg-slate-50/50 transition-colors">
                        <td className="px-4 py-3 text-center text-slate-400 font-medium">{idx + 1}</td>
                        <td className="px-4 py-3 font-bold text-slate-800">{item.word}</td>
                        <td className="px-4 py-3 text-center">
                          <span className="inline-block px-2 py-1 bg-indigo-50 text-indigo-700 font-bold text-[10px] rounded-lg">
                            {item.pos}
                          </span>
                        </td>
                        <td className="px-4 py-3 font-medium text-slate-600">{item.meaning}</td>
                        <td className="px-4 py-3 font-mono text-xs text-slate-400 truncate max-w-[100px]" title={item.id}>
                          {item.id}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>

            </div>
          </Dialog.Content>
        </div>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
