import React, { useState, useEffect, useRef } from 'react';
import * as Dialog from '@radix-ui/react-dialog';
import { ZoomIn, ZoomOut, RotateCcw, X, Eye, Move, ChevronLeft, ChevronRight } from 'lucide-react';

/**
 * Normalizes BBox array [ymin, xmin, ymax, xmax] (0~1000 or 0~100 scale) to percentage values (0~100)
 */
export function parseBBoxPercentages(bbox, bboxScale = 1000, refWidth = 0, refHeight = 0) {
  if (!bbox || !Array.isArray(bbox) || bbox.length < 4) {
    return { top: 0, left: 0, width: 0, height: 0, isValid: false };
  }
  let [ymin, xmin, ymax, xmax] = bbox.map(v => Number(v) || 0);
  if (ymin === 0 && xmin === 0 && ymax === 0 && xmax === 0) {
    return { top: 0, left: 0, width: 0, height: 0, isValid: false };
  }

  // Smart Scale Auto-Detection:
  // If maxVal <= 100, the coordinates are ALREADY 0~100 percentage values.
  // If maxVal > 100, the coordinates are 0~1000 scale and MUST be divided by 10.
  const maxVal = Math.max(ymin, xmin, ymax, xmax);
  if (maxVal > 100) {
    ymin /= 10.0;
    xmin /= 10.0;
    ymax /= 10.0;
    xmax /= 10.0;
  }

  const top = Math.max(0, Math.min(100, ymin));
  const left = Math.max(0, Math.min(100, xmin));
  const width = Math.max(0.1, Math.min(100 - left, xmax - xmin));
  const height = Math.max(0.1, Math.min(100 - top, ymax - ymin));
  return { top, left, width, height, isValid: true };
}

/**
 * Cropped mini evidence thumbnail for table rows using HTML5 Canvas
 */
export function CroppedEvidenceThumbnail({ imageUrl, bbox, bboxScale, imageWidth, imageHeight, word, onClick }) {
  const canvasRef = useRef(null);
  const [imageLoaded, setImageLoaded] = useState(false);

  useEffect(() => {
    if (!imageUrl) return;
    const img = new Image();
    img.crossOrigin = 'anonymous';
    img.src = imageUrl;

    img.onload = () => {
      setImageLoaded(true);
      const canvas = canvasRef.current;
      if (!canvas) return;

      const ctx = canvas.getContext('2d');
      const iw = img.naturalWidth;
      const ih = img.naturalHeight;

      let { top, left, width, height, isValid } = parseBBoxPercentages(bbox, bboxScale, imageWidth, imageHeight);
      
      if (!isValid) {
        top = 10;
        left = 5;
        width = 90;
        height = 20;
      }

      const ymin = top;
      const xmin = left;
      const ymax = top + height;
      const xmax = left + width;

      // Add padding margin around the target word bbox
      const padY = Math.max(2, (ymax - ymin) * 0.3);
      const padX = Math.max(2, (xmax - xmin) * 0.2);

      const cropYmin = Math.max(0, ymin - padY);
      const cropXmin = Math.max(0, xmin - padX);
      const cropYmax = Math.min(100, ymax + padY);
      const cropXmax = Math.min(100, xmax + padX);

      // Convert percentage (0-100) to actual pixel coordinates
      const sx = (cropXmin / 100) * iw;
      const sy = (cropYmin / 100) * ih;
      const sw = Math.max(1, ((cropXmax - cropXmin) / 100) * iw);
      const sh = Math.max(1, ((cropYmax - cropYmin) / 100) * ih);

      // Calculate natural aspect ratio to fit canvas height to container
      const cropAspect = sw / sh;
      canvas.height = 60;
      canvas.width = Math.max(80, Math.min(300, Math.round(60 * cropAspect)));

      ctx.clearRect(0, 0, canvas.width, canvas.height);
      ctx.drawImage(img, sx, sy, sw, sh, 0, 0, canvas.width, canvas.height);
    };

    img.onerror = () => {
      setImageLoaded(false);
    };
  }, [imageUrl, bbox]);

  return (
    <div 
      onClick={onClick}
      className="relative h-12 max-w-[200px] rounded-xl overflow-hidden border border-slate-700 bg-slate-900 cursor-pointer group hover:border-sky-400 transition-all shadow-md shrink-0 mx-auto flex items-center justify-center"
      title={`Click to open interactive zoom view for '${word}'`}
    >
      <canvas ref={canvasRef} className="h-full w-auto object-contain" />
      
      {!imageLoaded && (
        <div className="absolute inset-0 bg-slate-900 flex items-center justify-center text-slate-500 text-[10px] font-mono">
          Loading...
        </div>
      )}

      <div className="absolute inset-0 border border-rose-500/50 bg-rose-500/10 pointer-events-none group-hover:bg-rose-500/20 transition-colors" />
      <div className="absolute inset-0 bg-slate-950/75 opacity-0 group-hover:opacity-100 flex items-center justify-center text-sky-300 gap-1 text-[11px] font-bold transition-opacity">
        <Eye className="w-3.5 h-3.5" /> Inspect
      </div>
    </div>
  );
}

/**
 * Radix UI Driven Full Interactive Red-Box Zoom & Pan Modal Viewer
 */
export function InteractiveRedBoxModal({ wordItem, words = [], bboxScale, imageUrl, getImageUrl, onSelectWord, onClose }) {
  const [zoom, setZoom] = useState(1.0);
  const [position, setPosition] = useState({ x: 0, y: 15 });
  const [isDragging, setIsDragging] = useState(false);
  const [dragStart, setDragStart] = useState({ x: 0, y: 0 });
  const containerRef = useRef(null);

  // Find index of active wordItem in words array
  const currentIndex = Array.isArray(words) ? words.findIndex(w => (w.word === wordItem?.word && w.no === wordItem?.no) || w === wordItem) : -1;
  const hasPrev = currentIndex > 0;
  const hasNext = currentIndex >= 0 && currentIndex < words.length - 1;

  const currentImageUrl = getImageUrl ? getImageUrl(wordItem) : imageUrl;

  // Reset zoom & position when active wordItem changes
  useEffect(() => {
    setZoom(1.0);
    setPosition({ x: 0, y: 15 });
  }, [wordItem]);

  // Keyboard navigation shortcuts (ArrowLeft, ArrowRight, Escape)
  useEffect(() => {
    const handleKeyDown = (e) => {
      if (e.key === 'ArrowLeft' && hasPrev) {
        e.preventDefault();
        onSelectWord && onSelectWord(words[currentIndex - 1]);
      } else if (e.key === 'ArrowRight' && hasNext) {
        e.preventDefault();
        onSelectWord && onSelectWord(words[currentIndex + 1]);
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [currentIndex, hasPrev, hasNext, words, onSelectWord]);

  const bbox = wordItem?.bbox || wordItem?.bBox;
  const { top, left, width, height } = parseBBoxPercentages(bbox);

  const handleZoomIn = () => setZoom(prev => Math.min(4.0, prev + 0.3));
  const handleZoomOut = () => setZoom(prev => Math.max(0.8, prev - 0.3));
  const handleReset = () => {
    setZoom(1.0);
    setPosition({ x: 0, y: 15 });
  };

  const handleMouseDown = (e) => {
    setIsDragging(true);
    setDragStart({ x: e.clientX - position.x, y: e.clientY - position.y });
  };

  const handleMouseMove = (e) => {
    if (!isDragging) return;
    setPosition({
      x: e.clientX - dragStart.x,
      y: e.clientY - dragStart.y
    });
  };

  const handleMouseUp = () => setIsDragging(false);

  // Native non-passive wheel listener to allow e.preventDefault() without browser warnings
  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const onWheelNonPassive = (e) => {
      e.preventDefault();
      const delta = e.deltaY < 0 ? 0.15 : -0.15;
      setZoom(prev => Math.max(0.8, Math.min(4.0, prev + delta)));
    };

    container.addEventListener('wheel', onWheelNonPassive, { passive: false });
    return () => container.removeEventListener('wheel', onWheelNonPassive);
  }, []);

  return (
    <Dialog.Root open={true} onOpenChange={(open) => !open && onClose()}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-50 bg-slate-950/80 backdrop-blur-md radix-overlay-anim" />

        <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
          <Dialog.Content className="w-full max-w-4xl h-[72vh] bg-white border border-slate-200 rounded-3xl shadow-2xl overflow-hidden flex flex-col glass-panel radix-content-anim focus:outline-none my-auto">
            
            {/* Modal Header with Prev/Next Navigation */}
            <div className="flex items-center justify-between px-6 py-4 border-b border-slate-800 bg-slate-900/80">
              <div className="flex items-center gap-3">
                <div className="w-9 h-9 rounded-xl bg-red-500/10 border border-red-500/30 flex items-center justify-center text-red-400">
                  <Eye className="w-5 h-5" />
                </div>
                <div>
                  <div className="flex items-center gap-2">
                    <Dialog.Title className="text-base font-extrabold text-slate-100">
                      {wordItem?.word}
                    </Dialog.Title>
                    <span className="px-2 py-0.5 rounded-md bg-purple-500/20 text-purple-300 font-bold border border-purple-500/30 text-xs">
                      {wordItem?.pos}
                    </span>
                  </div>
                  <p className="text-xs text-slate-400 mt-0.5">{wordItem?.meaning}</p>
                </div>
              </div>

              {/* Controls & Word Navigator */}
              <div className="flex items-center gap-3">
                {/* Prev / Next Word Navigator */}
                {words.length > 1 && (
                  <div className="flex items-center gap-1 bg-slate-950/80 p-1 rounded-2xl border border-slate-800">
                    <button
                      onClick={() => hasPrev && onSelectWord && onSelectWord(words[currentIndex - 1])}
                      disabled={!hasPrev}
                      className={`p-1.5 rounded-xl transition-all flex items-center gap-1 text-xs font-bold ${
                        hasPrev 
                          ? 'hover:bg-slate-800 text-sky-400 hover:text-sky-300' 
                          : 'text-slate-600 cursor-not-allowed'
                      }`}
                      title="Previous Word (Left Arrow)"
                    >
                      <ChevronLeft className="w-4 h-4" />
                      <span className="hidden sm:inline">Prev</span>
                    </button>

                    <span className="text-[11px] font-mono font-extrabold text-slate-400 px-2 min-w-[50px] text-center select-none">
                      {currentIndex + 1} / {words.length}
                    </span>

                    <button
                      onClick={() => hasNext && onSelectWord && onSelectWord(words[currentIndex + 1])}
                      disabled={!hasNext}
                      className={`p-1.5 rounded-xl transition-all flex items-center gap-1 text-xs font-bold ${
                        hasNext 
                          ? 'hover:bg-slate-800 text-sky-400 hover:text-sky-300' 
                          : 'text-slate-600 cursor-not-allowed'
                      }`}
                      title="Next Word (Right Arrow)"
                    >
                      <span className="hidden sm:inline">Next</span>
                      <ChevronRight className="w-4 h-4" />
                    </button>
                  </div>
                )}

                {/* Zoom Controls */}
                <div className="flex items-center gap-1.5 bg-slate-950/80 p-1.5 rounded-2xl border border-slate-800">
                  <button onClick={handleZoomOut} className="p-1.5 rounded-xl hover:bg-slate-800 text-slate-300">
                    <ZoomOut className="w-4 h-4" />
                  </button>
                  <span className="text-xs font-mono font-bold text-cyan-400 px-2 min-w-[45px] text-center">
                    {Math.round(zoom * 100)}%
                  </span>
                  <button onClick={handleZoomIn} className="p-1.5 rounded-xl hover:bg-slate-800 text-slate-300">
                    <ZoomIn className="w-4 h-4" />
                  </button>
                  <div className="w-px h-4 bg-slate-800 my-auto" />
                  <button onClick={handleReset} className="p-1.5 rounded-xl hover:bg-slate-800 text-slate-400 hover:text-slate-200">
                    <RotateCcw className="w-4 h-4" />
                  </button>
                </div>

                <Dialog.Close className="w-9 h-9 rounded-full bg-slate-800/80 hover:bg-slate-700 border border-slate-700/60 flex items-center justify-center text-slate-300 hover:text-white transition-colors">
                  <X className="w-5 h-5" />
                </Dialog.Close>
              </div>
            </div>

          {/* Pan & Zoom Canvas */}
          <div 
            ref={containerRef}
            onMouseDown={handleMouseDown}
            onMouseMove={handleMouseMove}
            onMouseUp={handleMouseUp}
            onMouseLeave={handleMouseUp}
            className={`flex-1 relative overflow-hidden bg-slate-950 flex items-center justify-center select-none ${
              isDragging ? 'cursor-grabbing' : 'cursor-grab'
            }`}
          >
            <div className="absolute inset-0 bg-[radial-gradient(#1e293b_1px,transparent_1px)] [background-size:16px_16px] opacity-40 pointer-events-none" />

            <div 
              style={{
                transform: `translate(${position.x}px, ${position.y}px) scale(${zoom})`,
                transition: isDragging ? 'none' : 'transform 0.15s cubic-bezier(0.16, 1, 0.3, 1)',
                transformOrigin: 'center center'
              }}
              className="relative inline-block"
            >
              <img 
                src={currentImageUrl} 
                alt="Source Evidence" 
                className="h-[calc(72vh-110px)] max-h-full max-w-[80vw] w-auto rounded-lg shadow-2xl border border-slate-800 pointer-events-none object-contain mt-1"
              />
              {(() => {
                const { top, left, width, height, isValid } = parseBBoxPercentages(wordItem?.bbox, bboxScale || wordItem?.bboxScale, wordItem?.imageWidth, wordItem?.imageHeight);
                return isValid ? (
                  <div
                    style={{
                      top: `${top}%`,
                      left: `${left}%`,
                      width: `${width}%`,
                      height: `${height}%`
                    }}
                    className="absolute border-2 border-rose-500 bg-rose-500/25 shadow-[0_0_15px_rgba(244,63,94,0.7)] rounded-sm pointer-events-none transition-all duration-300"
                  >
                    <div className="absolute -top-6 left-0 bg-rose-600 text-white text-[10px] font-black px-2 py-0.5 rounded shadow-lg flex items-center gap-1 whitespace-nowrap">
                      <span>{wordItem?.word}</span>
                      <span className="opacity-80 text-[9px]">({wordItem?.pos})</span>
                    </div>
                  </div>
                ) : null;
              })()}
            </div>

            <div className="absolute bottom-4 left-4 pointer-events-none bg-slate-900/80 backdrop-blur-md px-3 py-1.5 rounded-xl border border-slate-800 text-xs text-slate-400 flex items-center gap-2">
              <Move className="w-3.5 h-3.5 text-cyan-400" />
              <span>Drag to pan | Wheel to zoom</span>
            </div>
          </div>

        </Dialog.Content>
        </div>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
