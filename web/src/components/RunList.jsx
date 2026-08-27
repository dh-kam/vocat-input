import React, { useState, useEffect, useRef } from 'react';
import * as Dialog from '@radix-ui/react-dialog';
import { Layers, Plus, Search, CheckCircle2, Loader2, FileText, ChevronRight, ChevronLeft, Clock, AlertCircle, Trash2, Calendar } from 'lucide-react';

const ITEMS_PER_PAGE = 15;

const formatDate = (dateStr) => {
  if (!dateStr) return 'Just now';
  const d = new Date(dateStr);
  if (isNaN(d.getTime())) return dateStr;
  const year = d.getFullYear();
  const month = String(d.getMonth() + 1).padStart(2, '0');
  const day = String(d.getDate()).padStart(2, '0');
  const hours = String(d.getHours()).padStart(2, '0');
  const mins = String(d.getMinutes()).padStart(2, '0');
  return `${year}.${month}.${day} ${hours}:${mins}`;
};

export default function RunList({ runs, selectedRunId, onSelectRun, onNewRun, onDeleteRun }) {
  const [filter, setFilter] = useState('ALL');
  const [searchTerm, setSearchTerm] = useState('');
  const [currentPage, setCurrentPage] = useState(1);
  const [deletingRun, setDeletingRun] = useState(null);
  const [isScrolling, setIsScrolling] = useState(false);
  const scrollTimeoutRef = useRef(null);

  const handleScrollActivity = () => {
    setIsScrolling(true);
    if (scrollTimeoutRef.current) {
      clearTimeout(scrollTimeoutRef.current);
    }
    scrollTimeoutRef.current = setTimeout(() => {
      setIsScrolling(false);
    }, 2000);
  };

  const filteredRuns = runs.filter(run => {
    const matchesFilter = 
      filter === 'ALL' || 
      (filter === 'COMPLETED' && run.status === 'COMPLETED') ||
      (filter === 'PROGRESS' && run.status !== 'COMPLETED');
    const matchesSearch = 
      run.id.toLowerCase().includes(searchTerm.toLowerCase()) ||
      (run.title && run.title.toLowerCase().includes(searchTerm.toLowerCase())) ||
      run.ocrProvider.toLowerCase().includes(searchTerm.toLowerCase());
    return matchesFilter && matchesSearch;
  });

  // Reset to page 1 on filter/search change
  useEffect(() => {
    setCurrentPage(1);
  }, [filter, searchTerm]);

  const totalPages = Math.ceil(filteredRuns.length / ITEMS_PER_PAGE) || 1;
  const startIndex = (currentPage - 1) * ITEMS_PER_PAGE;
  const paginatedRuns = filteredRuns.slice(startIndex, startIndex + ITEMS_PER_PAGE);

  return (
    <div className="flex flex-col h-full space-y-5">
      
      {/* Header & New Run Button */}
      <div className="flex items-center justify-between pb-3 border-b border-slate-200">
        <div className="flex items-center gap-3">
          <div className="w-10 h-10 rounded-2xl bg-sky-100 border border-sky-200 flex items-center justify-center text-sky-700">
            <Layers className="w-5 h-5" />
          </div>
          <div>
            <h2 className="text-base font-extrabold text-slate-900 uppercase tracking-wider">Conversion Runs</h2>
            <p className="text-xs font-semibold text-slate-500">{runs.length} Active Workflows</p>
          </div>
        </div>

        <button
          onClick={onNewRun}
          className="px-4 py-2.5 rounded-xl bg-gradient-to-r from-sky-600 to-indigo-600 hover:from-sky-700 hover:to-indigo-700 text-white text-sm font-extrabold flex items-center gap-2 shadow-md shadow-sky-500/20 transition-all"
        >
          <Plus className="w-4.5 h-4.5 stroke-[3]" />
          <span>New Run</span>
        </button>
      </div>

      {/* Search Input */}
      <div className="relative">
        <Search className="w-4.5 h-4.5 absolute left-4 top-3.5 text-slate-400 pointer-events-none" />
        <input
          type="text"
          placeholder="Filter runs by Title, ID or provider..."
          value={searchTerm}
          onChange={(e) => setSearchTerm(e.target.value)}
          className="w-full rounded-xl pl-11 pr-4 py-3 text-sm placeholder-slate-400 focus:outline-none focus:ring-2 focus:ring-sky-500/20 shadow-sm transition-all"
          style={{ backgroundColor: 'var(--card-bg)', color: 'var(--text-main)', borderColor: 'var(--border-color)', border: '1px solid var(--border-color)' }}
        />
      </div>

      {/* Filter Tabs */}
      <div className="flex items-center gap-1.5 p-1.5 bg-slate-200/60 border border-slate-300/80 rounded-2xl">
        {[
          { id: 'ALL', label: 'All Runs' },
          { id: 'COMPLETED', label: 'Completed' },
          { id: 'PROGRESS', label: 'In Progress' }
        ].map(tab => (
          <button
            key={tab.id}
            onClick={() => setFilter(tab.id)}
            className={`flex-1 py-2 text-xs font-extrabold rounded-xl transition-all ${
              filter === tab.id
                ? 'bg-white text-sky-800 shadow-sm border border-slate-200'
                : 'text-slate-600 hover:text-slate-900'
            }`}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {/* Run List Stream */}
      <div
        onScroll={handleScrollActivity}
        onMouseMove={handleScrollActivity}
        className={`flex-1 overflow-y-auto pr-1 space-y-3.5 max-h-[calc(100vh-290px)] auto-fade-scrollbar ${isScrolling ? 'is-scrolling' : ''}`}
      >
        {filteredRuns.length === 0 ? (
          <div className="flex flex-col items-center justify-center p-8 text-center border border-dashed border-slate-300 rounded-2xl bg-white/60">
            <AlertCircle className="w-8 h-8 text-slate-400 mb-2" />
            <p className="text-sm font-semibold text-slate-600">No matching conversion runs</p>
            <p className="text-xs text-slate-500 mt-1">Create a new run to get started</p>
          </div>
        ) : (
          paginatedRuns.map((run) => {
            const isSelected = selectedRunId === run.id;
            const isFailed = run.status === 'FAILED';
            const isDone = run.status === 'COMPLETED' || (run.progress >= 100 && !isFailed);
            const isInProg = !isDone && !isFailed && (
              run.status === 'OCR_IN_PROGRESS' ||
              run.status === 'MERGING_CONVERTING' ||
              (run.progress > 0 && run.progress < 100)
            );

            return (
              <div
                key={run.id}
                className={`relative rounded-2xl transition-all duration-300 ${
                  isInProg ? 'animate-neon-glow rounded-2xl' : ''
                }`}
              >
                <div
                  onClick={() => onSelectRun(run.id)}
                  className={`p-5 rounded-[16px] cursor-pointer transition-all duration-200 relative group overflow-hidden ${
                    isFailed
                      ? 'border border-rose-400 bg-rose-50/40 ring-2 ring-rose-500/20 shadow-md shadow-rose-500/10'
                      : isDone
                      ? isSelected
                        ? 'border-2 border-sky-500 bg-sky-50/70 shadow-lg shadow-sky-500/10 ring-2 ring-sky-500/20'
                        : 'border border-sky-200/70 bg-sky-50/40 hover:bg-sky-50/80 shadow-sm'
                      : isSelected
                      ? 'border-2 border-sky-500 bg-white shadow-lg shadow-sky-500/10 ring-2 ring-sky-500/20'
                      : 'border border-slate-200/90 bg-white hover:border-slate-300 shadow-sm'
                  }`}
                >
                  {/* Active / Status Indicator Strip */}
                  {isFailed ? (
                    <div className="absolute top-0 left-0 bottom-0 w-1.5 bg-rose-500 rounded-l-2xl" />
                  ) : isSelected ? (
                    <div className="absolute top-0 left-0 bottom-0 w-1.5 bg-gradient-to-b from-sky-500 to-indigo-600 rounded-l-2xl" />
                  ) : null}

                  <div className="flex items-start justify-between mb-2">
                    <div className="flex items-center gap-2.5">
                      <span className={`text-sm font-bold transition-colors line-clamp-1 ${isFailed ? 'text-rose-900' : 'text-slate-900 group-hover:text-sky-700'}`}>
                        {run.title || run.id}
                      </span>
                      <span className={`px-2.5 py-0.5 rounded-full text-xs font-bold uppercase tracking-wider shrink-0 ${
                        isFailed 
                          ? 'bg-rose-100 text-rose-800 border border-rose-300' 
                          : isDone
                          ? 'bg-sky-100/70 text-sky-800 border border-sky-200/80'
                          : 'bg-slate-100 text-slate-700 border border-slate-300'
                      }`}>
                        {run.ocrProvider} {run.ocrModel ? `· ${run.ocrModel.replace('amazon.', '').replace('us.anthropic.', '').replace(':0', '')}` : ''}
                      </span>
                    </div>

                    <div className="flex items-center gap-1.5 shrink-0">
                      <button
                        type="button"
                        onClick={(e) => {
                          e.stopPropagation();
                          setDeletingRun(run);
                        }}
                        title="Delete run and associated files"
                        className="p-1 rounded-lg text-slate-400 hover:text-rose-600 hover:bg-rose-50 opacity-0 group-hover:opacity-100 transition-all"
                      >
                        <Trash2 className="w-4 h-4" />
                      </button>
                      <ChevronRight className={`w-5 h-5 transition-transform ${isFailed ? 'text-rose-500' : isSelected ? 'text-sky-600 translate-x-0.5' : 'text-slate-400 group-hover:text-slate-600'}`} />
                    </div>
                  </div>

                  {/* Info row */}
                  <div className="flex items-center justify-between text-xs font-medium text-slate-600 mt-2">
                    <span className="flex items-center gap-1.5">
                      <FileText className={`w-4 h-4 ${isFailed ? 'text-rose-500' : 'text-sky-600'}`} />
                      <span className="font-bold text-slate-800">{run.images?.length || 0} Images</span>
                      <span className="text-slate-400">·</span>
                      <span className={`font-extrabold ${isFailed ? 'text-rose-600' : 'text-sky-700'}`}>{run.words?.length || 0} Words</span>
                    </span>
                    <span className="flex items-center gap-1.5 text-xs font-semibold text-slate-500 bg-slate-100/90 px-2 py-0.5 rounded-md border border-slate-200/80 shrink-0">
                      <Calendar className="w-3.5 h-3.5 text-slate-400" />
                      {formatDate(run.createdAt)}
                    </span>
                  </div>

                  {/* Show Progress Bar & Status Badge ONLY when NOT completed */}
                  {!isDone && (
                    <>
                      <div className="mt-3.5 pt-3 border-t border-slate-100 flex items-center justify-between">
                        <div className="flex items-center gap-2">
                          {isFailed ? (
                            <span className="inline-flex items-center gap-1.5 text-xs font-extrabold text-rose-600">
                              <AlertCircle className="w-4 h-4 text-rose-600" /> Failed
                            </span>
                          ) : isInProg ? (
                            <span className="inline-flex items-center gap-1.5 text-xs font-extrabold text-sky-700">
                              <Loader2 className="w-4 h-4 text-sky-600 animate-spin" /> In Progress
                            </span>
                          ) : (
                            <span className="inline-flex items-center gap-1 text-xs font-bold text-slate-500">
                              Created
                            </span>
                          )}
                        </div>

                        <span className={`text-sm font-mono font-black ${isFailed ? 'text-rose-600' : 'text-sky-700'}`}>
                          {isFailed ? 'ERR' : `${run.progress || 0}%`}
                        </span>
                      </div>

                      {/* Micro Progress Line */}
                      <div className="w-full h-1.5 bg-slate-100 rounded-full mt-2.5 overflow-hidden">
                        <div 
                          className={`h-full transition-all duration-300 ${
                            isFailed 
                              ? 'bg-rose-500' 
                              : 'bg-gradient-to-r from-sky-500 to-indigo-600'
                          }`}
                          style={{ width: `${isFailed ? 100 : (run.progress || 0)}%` }}
                        />
                      </div>
                    </>
                  )}
                </div>
              </div>
            );
          })
        )}
      </div>

      {/* Pagination Bar */}
      {totalPages > 1 && (
        <div className="flex items-center justify-between pt-3 border-t border-slate-200 text-xs font-semibold text-slate-600">
          <button
            type="button"
            disabled={currentPage === 1}
            onClick={() => setCurrentPage(prev => Math.max(prev - 1, 1))}
            className="px-3 py-1.5 rounded-xl border border-slate-200 bg-white hover:bg-slate-50 disabled:opacity-40 disabled:cursor-not-allowed flex items-center gap-1 shadow-xs transition-all cursor-pointer"
          >
            <ChevronLeft className="w-4 h-4" /> Prev
          </button>

          <div className="flex items-center gap-1 overflow-x-auto max-w-[140px] scrollbar-none">
            {Array.from({ length: totalPages }, (_, i) => i + 1).map(page => (
              <button
                key={page}
                type="button"
                onClick={() => setCurrentPage(page)}
                className={`w-7 h-7 rounded-xl font-bold transition-all text-xs flex items-center justify-center cursor-pointer shrink-0 ${
                  currentPage === page
                    ? 'bg-sky-600 text-white shadow-sm ring-2 ring-sky-600/30'
                    : 'bg-white border border-slate-200 text-slate-700 hover:bg-slate-50'
                }`}
              >
                {page}
              </button>
            ))}
          </div>

          <button
            type="button"
            disabled={currentPage === totalPages}
            onClick={() => setCurrentPage(prev => Math.min(prev + 1, totalPages))}
            className="px-3 py-1.5 rounded-xl border border-slate-200 bg-white hover:bg-slate-50 disabled:opacity-40 disabled:cursor-not-allowed flex items-center gap-1 shadow-xs transition-all cursor-pointer"
          >
            Next <ChevronRight className="w-4 h-4" />
          </button>
        </div>
      )}

      {/* Delete Confirmation Modal */}
      {deletingRun && (
        <Dialog.Root open={true} onOpenChange={(open) => !open && setDeletingRun(null)}>
          <Dialog.Portal>
            <Dialog.Overlay className="fixed inset-0 z-50 bg-slate-950/70 backdrop-blur-sm radix-overlay-anim" />
            <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
              <Dialog.Content className="w-full max-w-md bg-white border border-slate-200 rounded-3xl p-6 shadow-2xl radix-content-anim focus:outline-none">
                <div className="flex items-center gap-3.5 mb-4">
                  <div className="w-11 h-11 rounded-2xl bg-rose-100 border border-rose-200 flex items-center justify-center text-rose-600">
                    <Trash2 className="w-5 h-5" />
                  </div>
                  <div>
                    <Dialog.Title className="text-lg font-black text-slate-900">
                      Delete Workflow Run?
                    </Dialog.Title>
                    <Dialog.Description className="text-xs font-semibold text-slate-500 mt-0.5">
                      This action cannot be undone.
                    </Dialog.Description>
                  </div>
                </div>

                <p className="text-sm font-medium text-slate-700 bg-slate-50 p-3.5 rounded-2xl border border-slate-200 mb-5">
                  Are you sure you want to permanently delete <strong className="text-slate-900 font-bold">{deletingRun.title || deletingRun.id}</strong>? All uploaded images, extracted JSON, and DOC test sheet files will be removed.
                </p>

                <div className="flex items-center justify-end gap-2.5">
                  <button
                    type="button"
                    onClick={() => setDeletingRun(null)}
                    className="px-4 py-2.5 rounded-xl text-xs font-bold bg-slate-100 hover:bg-slate-200 text-slate-700 transition-colors"
                  >
                    Cancel
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      if (onDeleteRun) onDeleteRun(deletingRun.id);
                      setDeletingRun(null);
                    }}
                    className="px-5 py-2.5 rounded-xl text-xs font-black bg-rose-600 hover:bg-rose-700 text-white shadow-md shadow-rose-500/20 transition-all flex items-center gap-1.5"
                  >
                    <Trash2 className="w-4 h-4" /> Delete Permanently
                  </button>
                </div>
              </Dialog.Content>
            </div>
          </Dialog.Portal>
        </Dialog.Root>
      )}

    </div>
  );
}
