package org.cuit.epoch.controller;

import cn.dev33.satoken.stp.StpUtil;
import cn.dev33.satoken.util.SaResult;
import cn.hutool.core.bean.BeanUtil;
import com.baomidou.mybatisplus.core.conditions.query.LambdaQueryWrapper;
import com.baomidou.mybatisplus.core.toolkit.StringUtils;
import eu.bitwalker.useragentutils.UserAgent;
import io.swagger.annotations.Api;
import io.swagger.annotations.ApiOperation;
import lombok.extern.slf4j.Slf4j;
import org.cuit.epoch.dto.UserDetailDTO;
import org.cuit.epoch.dto.UserInfoDTO;
import org.cuit.epoch.entity.UserAuth;
import org.cuit.epoch.entity.UserInfo;
import org.cuit.epoch.exception.AppException;
import org.cuit.epoch.mapper.RoleMapper;
import org.cuit.epoch.mapper.UserAuthMapper;
import org.cuit.epoch.mapper.UserInfoMapper;
import org.cuit.epoch.service.RedisService;
import org.cuit.epoch.service.UserAuthService;
import org.cuit.epoch.util.IpUtils;
import org.cuit.epoch.util.PasswordUtils;
import org.cuit.epoch.util.Result;
import org.cuit.epoch.vo.strategy.login.WeiboLoginVO;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.web.bind.annotation.*;

import javax.annotation.PostConstruct;
import javax.annotation.Resource;
import javax.servlet.http.HttpServletRequest;
import javax.validation.Valid;
import java.time.LocalDateTime;
import java.time.ZoneId;
import java.util.List;
import java.util.Objects;
import java.util.Set;

import static org.cuit.epoch.enums.RedisPrefixConst.*;
import static org.cuit.epoch.enums.ZoneEnum.SHANGHAI;

/**
 * @author: ladidol
 * @date: 2022/11/16 15:35
 * @description:
 */
@RestController
@Slf4j
@Api(tags = "登录模块")
public class LoginController {

    @Autowired
    UserAuthService userAuthService;

    @Resource
    private HttpServletRequest request;


    @PostMapping("login")
    @ApiOperation(value = "邮箱密码登录")
    public Result<UserDetailDTO> login(String username, String password) {
        UserDetailDTO userDetailDTO = userAuthService.login(username, password);
        return Result.ok(userDetailDTO);
    }

    @RequestMapping("logout")
    @ApiOperation(value = "注销登录")
    public Result<String> logout() {
        userAuthService.logout();
        return Result.ok("注销成功");
    }


    /**
     * 微博登录
     *
     * @param weiBoLoginVO 微博登录信息
     * @return {@link Result< UserInfoDTO >} 用户信息
     */
    @ApiOperation(value = "微博登录")
    @PostMapping("/users/oauth/weibo")
    public Result<UserInfoDTO> weiboLogin(@Valid @RequestBody WeiboLoginVO weiBoLoginVO) {
        return Result.ok(userAuthService.weiboLogin(weiBoLoginVO));
    }

//    /**
//     * qq登录
//     *
//     * @param qqLoginVO qq登录信息
//     * @return {@link Result<UserInfoDTO>} 用户信息
//     */
//    @ApiOperation(value = "qq登录")
//    @PostMapping("/users/oauth/qq")
//    public Result<UserInfoDTO> qqLogin(@Valid @RequestBody QQLoginVO qqLoginVO) {
//        return Result.ok(userAuthService.qqLogin(qqLoginVO));
//    }


}