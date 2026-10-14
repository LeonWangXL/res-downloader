import type {AxiosResponse, InternalAxiosRequestConfig} from 'axios'
import axios from 'axios'
import {shouldAttachSessionToken} from './session'

interface RequestOptions {
    url: string
    method: 'get' | 'post' | 'put' | 'delete'
    params?: Record<string, any>
    data?: Record<string, any>
}

const instance = axios.create({
    baseURL: "/",
    timeout: 180000
})

instance.interceptors.request.use(
    (config: InternalAxiosRequestConfig<any>) => {
        if(shouldAttachSessionToken(config.url || '/',window.$baseUrl)) {
            config.headers.set("X-Resd-Token",window.$sessionToken || "")
        }
        return config
    },
    (error) => {
        return Promise.reject(error)
    }
)

instance.interceptors.response.use(
    (response: AxiosResponse) => {
        return response.data
    },
    (error) => {
        return Promise.reject(error);
    }
)

const request = ({url, method, params, data}: RequestOptions): Promise<any> => {
    return instance({url, method, params, data, baseURL: window.$baseUrl})
}

export default request
