import { isModalOpen, isTypingTarget } from './useShortcuts'

/**
 * Whether a view may move focus into its content right after it loads. True only when focus is
 * unplaced (nothing or body) or still on app navigation (an element inside `[data-nav]`, such as
 * the sidebar link that was just clicked). False for typing targets, anything else in the page,
 * and while a modal is open.
 */
export function shouldClaimFocus(): boolean {
  if (isModalOpen()) return false
  const active = document.activeElement
  if (!active || active === document.body) return true
  if (isTypingTarget(active)) return false
  return active.closest('[data-nav]') !== null
}
