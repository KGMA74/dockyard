import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

// Re-exported for back-compat; new code should import from '@/lib/format'.
export { relativeTime, formatDateTime, formatDate, formatBytes, shortDigest } from './format'
