// src/utils/pocketbase.js
import PocketBase from 'pocketbase';

const pocketbaseUrl = import.meta.env.PUBLIC_POCKETBASE_URL;

export const pb = new PocketBase(pocketbaseUrl);

// Optional: Make it globally accessible for simple <script> tags if needed,
// but importing is generally preferred.
// if (typeof window !== 'undefined') {
//   window.pb = pb;
// }

// Automatically refresh token
pb.autoRefreshThreshold = 30 * 60; // Refresh token 30 minutes before expiry

// Export auth store helper methods for SSR/middleware use
export const loadAuthFromCookie = (cookie) => {
    try {
        pb.authStore.loadFromCookie(cookie || '');
    } catch (error) {
        pb.authStore.clear();
    }
};

export const enrichAuthStore = async () => {
    if (pb.authStore.isValid) {
        try {
            await pb.collection('users').authRefresh();
        } catch (_) {
            pb.authStore.clear();
        }
    }
};

export const exportAuthToCookie = (options = {}) => {
    // Secure defaults for production, adjust if needed for local http
    const defaultOptions = {
        secure: import.meta.env.PROD,
        sameSite: 'Lax',
        httpOnly: false, // Needs to be false for client-side access
        path: '/',
        // maxAge: 60 * 60 * 24 * 7 // Example: 7 days
    };
    return pb.authStore.exportToCookie({ ...defaultOptions, ...options });
};