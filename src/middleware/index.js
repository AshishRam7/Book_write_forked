// src/middleware/index.ts
import { defineMiddleware } from 'astro/middleware';
import PocketBase from 'pocketbase';
import { sequence } from 'astro/middleware';

const pocketbaseUrl = import.meta.env.PUBLIC_POCKETBASE_URL;

// Middleware to initialize PocketBase client per request
const authMiddleware = defineMiddleware(async ({ locals, request }, next) => {
    // ... (authMiddleware code remains the same as before) ...
    locals.pb = new PocketBase(pocketbaseUrl);
    const cookie = request.headers.get('cookie') || '';
    try {
        locals.pb.authStore.loadFromCookie(cookie, 'pb_auth');
    } catch (_) {
        locals.pb.authStore.clear();
    }

    try {
        if (locals.pb.authStore.isValid) {
            await locals.pb.collection('users').authRefresh();
        }
    } catch (_) {
        locals.pb.authStore.clear();
    }

    const response = await next();

    const responseCookie = locals.pb.authStore.exportToCookie({
        secure: import.meta.env.PROD,
        httpOnly: false,
        sameSite: 'Lax',
        path: '/',
    });
    if (responseCookie.includes('pb_auth=')) {
         response.headers.append('set-cookie', responseCookie);
    } else {
         response.headers.append('set-cookie', 'pb_auth=; Path=/; Max-Age=0; SameSite=Lax');
    }
    return response;
});


// Middleware to protect routes
const routeProtectionMiddleware = defineMiddleware(async ({ locals, url, redirect }, next) => {
    const pathname = url.pathname;

    // --->>> THE CRUCIAL FIX <<---
    // Ignore API routes and internal Astro assets
    if (pathname.startsWith('/api/') || pathname.startsWith('/_astro/')) {
        // Let these requests pass through without redirection by this middleware
        return next();
    }
    // --->>> END OF FIX AREA <<<---


    const publicRoutes = ['/login'];
    const isPublicRoute = publicRoutes.some(path => pathname === path);
    const isLoggedIn = locals.pb?.authStore?.isValid ?? false;

    // If trying to access a protected route while not logged in, redirect to login
    if (!isPublicRoute && !isLoggedIn) {
        console.log(`[Middleware] Redirecting to /login from protected route: ${pathname}`);
        return redirect('/login', 302);
    }

    // Optional: Redirect logged-in users away from login page
    // if (isPublicRoute && isLoggedIn && pathname === '/login') {
    //     console.log(`[Middleware] Redirecting logged in user from ${pathname} to /`);
    //     return redirect('/', 302);
    // }

    // Allow access to the requested page
    // console.log(`[Middleware] Allowing access to: ${pathname}`);
    return next();
});

// Export the sequence of middleware
export const onRequest = sequence(authMiddleware, routeProtectionMiddleware);