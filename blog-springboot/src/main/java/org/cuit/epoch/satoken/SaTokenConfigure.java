package org.cuit.epoch.satoken;

import cn.dev33.satoken.context.SaHolder;
import cn.dev33.satoken.filter.SaServletFilter;
import cn.dev33.satoken.interceptor.SaInterceptor;
import cn.dev33.satoken.router.SaRouter;
import cn.dev33.satoken.stp.StpUtil;
import cn.dev33.satoken.util.SaResult;
import lombok.extern.slf4j.Slf4j;
import org.cuit.epoch.exception.AppException;
import org.cuit.epoch.handler.MySourceSafilterAuthStrategy;
import org.cuit.epoch.util.Result;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.web.servlet.config.annotation.InterceptorRegistry;
import org.springframework.web.servlet.config.annotation.WebMvcConfigurer;

import javax.annotation.Resource;
import javax.servlet.http.HttpServletRequest;


/**
 * [Sa-Token 权限认证] 配置类
 * 详细配置可以看这个：https://sa-token.dev33.cn/doc.html#/use/route-check
 *
 * @author kong
 */
@Configuration
@Slf4j
public class SaTokenConfigure implements WebMvcConfigurer {


    @Resource
    HttpServletRequest request;

    @Autowired
    MySourceSafilterAuthStrategy mySourceSafilterAuthStrategy;


    /**
     * 注册 [Sa-Token 拦截器]
     * DispatcherServlet 之后
     */
    @Override
    public void addInterceptors(InterceptorRegistry registry) {
        // 注册路由拦截器，自定义认证规则
        registry.addInterceptor(new SaInterceptor(handler -> {

            // 登录校验 -- 拦截所有路由，并排除/user/doLogin 用于开放登录
            SaRouter.match("/admin/**", "/login", r -> StpUtil.checkLogin());

            // 角色校验 -- 拦截以 admin 开头的路由，必须具备 admin 角色或者 super-admin 角色才可以通过认证
//            SaRouter.match("/admin/**", r -> StpUtil.checkRoleOr("admin", "test"));
            // 甚至你可以随意的写一个打印语句
            SaRouter.match("/**", r -> log.info("----啦啦啦跑了一个匿名接口了----路径为：" + request.getRequestURI()));

//			// 连缀写法
//			SaRouter.match("/**").check(r -> System.out.println("----啦啦啦----"));

        })).addPathPatterns("/**");
    }

    /**
     * 注册 [Sa-Token 全局过滤器]
     * DispatcherServlet 之前
     */
    @Bean
    public SaServletFilter getSaServletFilter() {
        return new SaServletFilter()

                // 指定 [拦截路由] 与 [放行路由]
                .addInclude("/admin/**")
                .addExclude("/favicon.ico")
                .addExclude("/login")
                .addExclude("/doc.html")

                // 认证函数: 每次请求执行
                .setAuth(mySourceSafilterAuthStrategy)
                // 异常处理函数：每次认证函数发生异常时执行此函数
                .setError(e -> {
                    // 2022/11/18 这里就是用户权限不足的时候
                    log.info(e.getMessage());
                    e.printStackTrace();
                    if (e instanceof AppException){
                        return Result.fail(e.getMessage());
                    }
                    return Result.fail(e.getMessage()+"：用户未登录");
                })

                // 前置函数：在每次认证函数之前执行
                .setBeforeAuth(r -> {
                    // ---------- 设置一些安全响应头 ----------
                    SaHolder.getResponse()
                            // 服务器名称
                            .setServer("sa-server")
                            // 是否可以在iframe显示视图： DENY=不可以 | SAMEORIGIN=同域下可以 | ALLOW-FROM uri=指定域名下可以
                            .setHeader("X-Frame-Options", "SAMEORIGIN")
                            // 是否启用浏览器默认XSS防护： 0=禁用 | 1=启用 | 1; mode=block 启用, 并在检查到XSS攻击时，停止渲染页面
                            .setHeader("X-XSS-Protection", "1; mode=block")
                            // 禁用浏览器内容嗅探
                            .setHeader("X-Content-Type-Options", "nosniff")
                    ;
                })
                ;
    }

}
